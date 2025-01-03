// Package webmention implements the receiving end of webmentions. 
package webmention

import (
	"fmt"
	"context"
	"golang.org/x/net/html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"strconv"
	"slices"
	mimelib "mime"
)

type (
	// Receiver is a http.Handler that takes care of processing webmentions.
	Receiver struct {
		enqueue       chan<- Mention
		dequeue       <-chan Mention
		notifiers     []Notifier
		httpClient    *http.Client
		shutdown      chan struct{}
		targetAccepts TargetAcceptsFunc
		mediaHandler  mediaRegister
		userAgent     string
	}

	mediaRegister []mediaHandler
	mediaHandler struct {
		name    string
		handler MediaHandler
		qweight float64
	}

	// A MediaHandler searches sourceData for the target link.
	// Only if an exact match is found a status of StatusLink and a nil error must be returned.
	// If no (exact) match is found, a status of StatusNoLink and a nil error must be returned.
	// If error is non-nil, it is treated as an internal error and the value of status is ignored.
	// On error, no listeners will be invoked.
	MediaHandler    func(sourceData io.Reader, target URL) (Status, error)
	ReceiverOption  func(*Receiver)
	Mention struct {
		Source, Target URL
		Status         Status
	}
	Status string
	TargetAcceptsFunc func(source, target URL) bool

	// A registered Notifier is informed of any valid webmentions.
	// This can be used to implement your own notifiers, e.g., to send a message on Discord, or XMPP.
	// The Status field of the mention needs to be checked:
	//   - StatusLink: the source (still, or newly) links to target
	//   - StatusNoLink: the source does not (anymore) link to target
	//   - StatusDeleted: the source got deleted
	Notifier interface {
		Receive(mention Mention)
	}

	// NotifierFunc adapts a function to an object that implements the Notifier interface.
	NotifierFunc func(mention Mention)
)

func (mr mediaRegister) Get(mime string) (MediaHandler, bool) {
	for _, h := range mr {
		if h.name == mime {
			return h.handler, true
		}
	}
	return nil, false
}

func (mr mediaRegister) String() string {
	var builder strings.Builder
	for i, h := range mr {
		if i > 0 {
			builder.WriteString(",")
		}
		if h.qweight == 1 {
			builder.WriteString(h.name)
		} else {
			builder.WriteString(fmt.Sprintf("%s;q=%s", h.name, strconv.FormatFloat(h.qweight, 'f', 3, 64)))
		}
	}
	return builder.String()
}

const (
	defaultRequestQueueSize = 100
)

const (
	StatusLink    Status = "source links to target"
	StatusNoLink         = "source does not link to target"
	StatusDeleted        = "source itself got deleted"
)

// Report may be reassigned to handle 'unhandled' errors
var Report = func(err error, mention Mention) {
}

func (f NotifierFunc) Receive(mention Mention) {
	f(mention)
}

func NewReceiver(opts ...ReceiverOption) *Receiver {
	queue := make(chan Mention, defaultRequestQueueSize)
	receiver := &Receiver{
		httpClient: http.DefaultClient,
		enqueue:    queue,
		dequeue:    queue,
		shutdown:   make(chan struct{}),
		targetAccepts: func(URL, URL) bool {
			return false
		},
		userAgent:  "Webmention (github.com/cvanloo/gowebmention)",
	}
	receiver.mediaHandler = mediaRegister{
		{name: "text/html", qweight: 1.0, handler: receiver.HtmlHandler},
		{name: "text/plain", qweight: 0.1, handler: receiver.PlainHandler},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(receiver)
		}
	}
	return receiver
}

// WithFetchUserAgent configures the user agent to be used when fetching a mention's source.
func WithFetchUserAgent(agent string) ReceiverOption {
	return func(r *Receiver) {
		r.userAgent = agent
	}
}

func WithNotifier(notifiers ...Notifier) ReceiverOption {
	return func(r *Receiver) {
		r.notifiers = append(r.notifiers, notifiers...)
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
//   - text/plain;q=1.0: PlainHandler
//   - text/html;q=0.1:  HtmlHandler
//
// To remove any of the default handlers, pass a nil handler.
func WithMediaHandler(mime string, qweight float64, handler MediaHandler) ReceiverOption {
	return func(r *Receiver) {
		if handler == nil {
			for i, h := range r.mediaHandler {
				if h.name == mime {
					r.mediaHandler = slices.Delete(r.mediaHandler, i, i+1)
					break
				}
			}
		} else {
			r.mediaHandler = append(r.mediaHandler, mediaHandler{
				name: mime,
				qweight: qweight,
				handler: handler,
			})
		}
	}
}

// Configure size of the request queue.
// The server will start returning http.StatusTooManyRequests when the request
// queue is full.
func WithQueueSize(size int) ReceiverOption {
	return func(r *Receiver) {
		queue := make(chan Mention, size)
		r.enqueue = queue
		r.dequeue = queue
	}
}

func (receiver *Receiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := receiver.handle(w, r); err != nil {
		if err, ok := err.(ErrorResponder); ok {
			if err.RespondError(w, r) {
				return
			}
		}
		slog.Error(err.Error(), "path", r.URL.EscapedPath(), "method", r.Method, "remote", r.RemoteAddr)
		http.Error(w, "internal server error", 500)
	}
}

func (receiver *Receiver) handle(w http.ResponseWriter, r *http.Request) error {
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

	if !receiver.targetAccepts(sourceURL, targetURL) {
		return BadRequest("target does not accept webmentions from this source")
	}

	select {
	case receiver.enqueue <- Mention{sourceURL, targetURL, StatusNoLink}:
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
// You may start multiple goroutines all running this function.
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
			Report(receiver.processMention(mention), mention)
		}
	}
}

// Shutdown causes the webmention service to stop accepting any new mentions.
// Mentions currently waiting in the request queue will still be processed, until ctx expires.
// The http server must be stopped first, ServeHTTP will panic otherwise.
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
			Report(receiver.processMention(mention), mention)
		}
	}
}

func (receiver *Receiver) processMention(mention Mention) error {
	log := slog.With(
		"function", "processMention",
		slog.Group("request_info",
			"mention", mention,
		),
	)

	mime := "text/plain"

	{
		req, err := http.NewRequest(http.MethodHead, mention.Source.String(), nil)
		if err != nil {
			log.Error(err.Error())
			return err
		}
		req.Header.Set("User-Agent", receiver.userAgent)
		req.Header.Set("Accept", receiver.mediaHandler.String())
		resp, err := receiver.httpClient.Do(req)
		if err != nil {
			log.Error(err.Error())
			return err
		}
		if resp.StatusCode == 410 {
			mention.Status = StatusDeleted
			// Processing should be idempotent
			for _, notifier := range receiver.notifiers {
				go notifier.Receive(mention)
			}
			return nil
		}
		if resp.StatusCode < 200 || resp.StatusCode > 300 {
			err = ErrSourceNotFound
			log.Error(err.Error())
			return err
		}
		contentHeader := resp.Header.Get("Content-Type")
		mediaType, _, err := mimelib.ParseMediaType(contentHeader)
		if err != nil {
			log.Error(err.Error(), "media_types", resp.Header.Get("Content-Type"))
			return err
		}
		mime = mediaType
	}

	{
		mediaHandler, hasHandler := receiver.mediaHandler.Get(mime)
		if !hasHandler {
			log.Error("no mime handler registered", "mime", mime)
			return fmt.Errorf("no mime handler registered for: %s", mime)
		}

		req, err := http.NewRequest(http.MethodGet, mention.Source.String(), nil)
		if err != nil {
			log.Error(err.Error())
			return err
		}
		req.Header.Set("User-Agent", receiver.userAgent)
		req.Header.Set("Accept", mime)
		resp, err := receiver.httpClient.Do(req)
		if err != nil {
			log.Error(err.Error())
			return err
		}

		handlerStatus, err := mediaHandler(resp.Body, mention.Target)
		if err != nil {
			log.Error(err.Error())
			return err
		}
		mention.Status = handlerStatus
	}

	// Processing should be idempotent
	slog.Info(fmt.Sprintf("sending to %d notifiers", len(receiver.notifiers)))
	for _, notifier := range receiver.notifiers {
		go notifier.Receive(mention)
	}

	return nil
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
	if err != nil {
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
