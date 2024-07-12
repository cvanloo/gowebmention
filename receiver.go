package webmention

import (
	"context"
	"github.com/elnormous/contenttype"
	"golang.org/x/net/html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

type (
	Receiver struct {
		enqueue       chan<- IncomingMention
		dequeue       <-chan IncomingMention
		notifiers     []Notifier
		httpClient    *http.Client
		shutdown      chan struct{}
		targetExists  TargetExistsFunc
		targetAccepts TargetAcceptsFunc
		//history       HistoryFunc
		mediaHandler map[string]MediaHandler // @todo: [:priority:]
	}

	// A MediaHandler searches sourceData for the target link.
	// Only if an exact match is found a status of StatusLink and a nil error must be returned.
	// If no (exact) match is found, a status of StatusNoLink and a nil error must be returned.
	// If error is non-nil, it is treated as an internal error and the value of status is ignored.
	// Generally, on error, no listeners will be invoked.
	MediaHandler    func(sourceData io.Reader, target URL) (Status, error)
	ReceiverOption  func(*Receiver)
	IncomingMention struct { // @todo: rename to just 'Mention'
		Source, Target URL
		Status         Status // @todo: add: Details map[string]???
	}
	TargetExistsFunc  func(target URL) bool // @todo: only one of those
	TargetAcceptsFunc func(source, target URL) bool
	Notifier          interface {
		Receive(mention IncomingMention)
	}
	Status string // @todo: not good that user defined handlers should only return two out of the three defined values

	// NotifierFunc adapts a function to an object that implements the Notifier interface.
	NotifierFunc func(mention IncomingMention)
)

const (
	defaultRequestQueueSize = 100
)

const (
	StatusLink    Status = "source links to target"
	StatusNoLink         = "source does not link to target"
	StatusDeleted        = "source itself got deleted"
)

func (f NotifierFunc) Receive(mention IncomingMention) {
	f(mention)
}

func NewReceiver(opts ...ReceiverOption) *Receiver {
	queue := make(chan IncomingMention, defaultRequestQueueSize)
	receiver := &Receiver{
		httpClient: http.DefaultClient,
		enqueue:    queue,
		dequeue:    queue,
		shutdown:   make(chan struct{}),
		targetExists: func(URL) bool {
			return false
		},
		targetAccepts: func(URL, URL) bool {
			return false
		},
		//history: func(source, target URL) History {
		//	return DisableHistory
		//},
	}
	receiver.mediaHandler = map[string]MediaHandler{
		"text/html":  receiver.HtmlHandler,
		"text/plain": receiver.PlainHandler,
	}
	for _, opt := range opts {
		opt(receiver)
	}
	return receiver
}

func WithNotifier(notifiers ...Notifier) ReceiverOption {
	return func(r *Receiver) {
		r.notifiers = append(r.notifiers, notifiers...)
	}
}

func WithExistsFunc(exists TargetExistsFunc) ReceiverOption {
	return func(r *Receiver) {
		r.targetExists = exists
	}
}

func WithAcceptsFunc(accepts TargetAcceptsFunc) ReceiverOption {
	return func(r *Receiver) {
		r.targetAccepts = accepts
	}
}

// Register a handler for a certain media type.
// If multiple handlers for the same type are registered, only the last handler will be considered.
// The default handlers are:
//   - text/plain: PlainHandler
//   - text/html:  HtmlHandler
//
// To remove any of the default handlers, pass a nil handler.
func WithMediaHandler(mime string, handler MediaHandler) ReceiverOption {
	return func(r *Receiver) {
		if handler == nil {
			delete(r.mediaHandler, mime)
		} else {
			r.mediaHandler[mime] = handler
		}
	}
}

func WithQueueSize(size int) ReceiverOption {
	return func(r *Receiver) {
		queue := make(chan IncomingMention, size)
		r.enqueue = queue
		r.dequeue = queue
	}
}

func (receiver *Receiver) WebmentionEndpoint(w http.ResponseWriter, r *http.Request) {
	if err := receiver.Handle(w, r); err != nil {
		if err, ok := err.(ErrorResponder); ok {
			if err.RespondError(w, r) {
				// @todo: log request either way as Info
				return
			}
		}
		slog.Error(err.Error(), "path", r.URL.EscapedPath(), "method", r.Method, "remote", r.RemoteAddr)
		http.Error(w, "internal server error", 500)
	}
}

func (receiver *Receiver) Handle(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		return MethodNotAllowed()
	}

	if err := r.ParseForm(); err != nil {
		return BadRequest(err.Error())
	}

	source, hasSource := r.PostForm["source"]
	if !hasSource {
		return BadRequest("missing form value: source")
	}
	target, hasTarget := r.PostForm["target"]
	if !hasTarget {
		return BadRequest("missing form value: target")
	}

	if len(source) != 1 {
		return BadRequest("malformed source argument")
	}
	if len(target) != 1 {
		return BadRequest("malformed target argument")
	}

	if source[0] == target[0] {
		return BadRequest("target must be different from source")
	}

	sourceURL, err := url.Parse(source[0])
	if err != nil {
		return BadRequest("source url is malformed")
	}
	targetURL, err := url.Parse(target[0])
	if err != nil {
		return BadRequest("target url is malformed")
	}

	if !(sourceURL.Scheme == "http" || sourceURL.Scheme == "https") {
		return BadRequest("source url scheme not supported (supported schemes are: http, https)")
	}
	if !(targetURL.Scheme == "http" || targetURL.Scheme == "https") {
		return BadRequest("target url scheme not supported (supported schemes are: http, https)")
	}

	if !receiver.targetExists(targetURL) {
		return BadRequest("target does not exist")
	}
	if !receiver.targetAccepts(sourceURL, targetURL) {
		return BadRequest("target does not accept webmentions")
	}

	select {
	case receiver.enqueue <- IncomingMention{sourceURL, targetURL, StatusNoLink}:
	default:
		return TooManyRequests()
	}

	w.WriteHeader(http.StatusAccepted)
	if _, err := w.Write([]byte("Thank you! Your Mention has been queued for processing.")); err != nil {
		return err
	}
	return nil
}

// ProcessMentions does not return until stopped by calling Shutdown.
// It is intended to run this function in its own goroutine.
func (receiver *Receiver) ProcessMentions() {
	// process queue until a shutdown is issued
	for {
		select {
		case <-receiver.shutdown:
			return
		case mention, ok := <-receiver.dequeue:
			if !ok {
				return
			}
			receiver.processMention(mention)
		}
	}
}

// Shutdown causes the webmention service to stop accepting any new mentions.
// Mentions currently waiting in the request queue will still be processed,
// until ctx expires.
// The http server (or whatever is invoking WebmentionEndpoint) must be stopped
// first, WebmentionEndpoint will panic otherwise.
func (receiver *Receiver) Shutdown(ctx context.Context) {
	// Finish processing queue until it is emptied or the shutdown context has expired.
	// Whichever happens first.
	close(receiver.shutdown)
	close(receiver.enqueue)
	for {
		select {
		case <-ctx.Done():
			return
		case mention, ok := <-receiver.dequeue:
			if !ok {
				return
			}
			receiver.processMention(mention)
		}
	}
}

func (receiver *Receiver) processMention(mention IncomingMention) {
	log := slog.With(
		"function", "processMention",
		slog.Group("request_info",
			"mention", mention,
		),
	)
	log.Info("started processing")

	//history := receiver.history(mention.Source, mention.Target)

	mime := "text/plain"

	{
		req, err := http.NewRequest(http.MethodHead, mention.Source.String(), nil)
		if err != nil {
			log.Error(err.Error())
			return
		}
		for mime := range receiver.mediaHandler { // @todo: [:priority:]
			req.Header.Add("Accept", mime)
		}
		resp, err := receiver.httpClient.Do(req)
		if err != nil {
			log.Error(err.Error())
			return
		}
		if resp.StatusCode == 410 {
			mention.Status = StatusDeleted
			// Processing should be idempotent
			for _, notifier := range receiver.notifiers {
				notifier.Receive(mention)
			}
			return
		}
		if resp.StatusCode < 200 || resp.StatusCode > 300 {
			log.Error(ErrSourceNotFound.Error())
			return
		}
		// @todo: calculate this only once at initialization
		availTypes := make([]contenttype.MediaType, 0, len(receiver.mediaHandler))
		for mime := range receiver.mediaHandler {
			availTypes = append(availTypes, contenttype.NewMediaType(mime))
		}
		mediaType, _, err := contenttype.GetAcceptableMediaTypeFromHeader(resp.Header.Get("Content-Type"), availTypes)
		if err != nil {
			log.Error(err.Error())
		}
		mime = mediaType.String()
	}

	handler, hasHandler := receiver.mediaHandler[mime]
	if !hasHandler {
		log.Error("no mime handler registered", "mime", mime)
		return
	}

	{
		req, err := http.NewRequest(http.MethodGet, mention.Source.String(), nil)
		if err != nil {
			log.Error(err.Error())
			return
		}
		req.Header.Set("Accept", mime)
		resp, err := receiver.httpClient.Do(req)
		if err != nil {
			log.Error(err.Error())
			return
		}

		handlerStatus, err := handler(resp.Body, mention.Target)
		if err != nil {
			log.Error(err.Error())
			return
		}
		mention.Status = handlerStatus
	}

	// Processing should be idempotent
	for _, notifier := range receiver.notifiers {
		notifier.Receive(mention)
	}
}

func (receiver *Receiver) PlainHandler(content io.Reader, target URL) (status Status, err error) {
	bs, err := io.ReadAll(content)
	if err != nil {
		return status, err
	}
	if !strings.Contains(string(bs), target.String()) {
		return StatusNoLink, nil
	}
	return StatusLink, nil
}

func (receiver *Receiver) HtmlHandler(content io.Reader, target URL) (status Status, err error) {
	doc, err := html.Parse(content)
	if err != nil { // @todo: be a bit fault tolerant in parsing html? like browsers are
		return status, err
	}

	var traverseHtml func(*html.Node) bool
	traverseHtml = func(node *html.Node) (found bool) {
		if node.Type == html.ElementNode {
			switch node.Data {
			case "a":
				fallthrough
			case "img":
				fallthrough
			case "video":
				href := findHref(node)
				if strings.ToLower(href) == strings.ToLower(target.String()) {
					return true
				}
				//case "p":
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling { // parse in depth-first order
			if traverseHtml(child) {
				return true
			}
		}
		return false
	}
	if !traverseHtml(doc) {
		return StatusNoLink, nil
	}
	return StatusLink, nil
}

func findHref(node *html.Node) (href string) {
	for _, a := range node.Attr {
		if a.Key == "href" { // @todo: what if there are multiple hrefs, for whatever reason?
			href = a.Val
			return
		}
	}
	return
}
