package webmention

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

type (
	Receiver struct {
		schemes    []Scheme
		enqueue    chan<- IncomingMention
		dequeue    <-chan IncomingMention
		listeners  []Listener
		httpClient *http.Client
		shutdown   chan struct{}
	}
	Scheme          string
	ReceiverOption  func(*Receiver)
	IncomingMention struct {
		Source, Target URL
	}
	Listener interface {
		Receive(mention IncomingMention, rel Relationship)
	}
	Relationship string
)

const (
	SourceLinksToTarget       Relationship = "source links to target"
	SourceUpdated             Relationship = "source got updated, still links to target"
	SourceRemovedTarget       Relationship = "source no longer links to target"
	SourceGotDeleted          Relationship = "source itself got deleted"
	SourceDoesNotLinkToTarget Relationship = "source does not link to target"
	SourceDoesNotExist        Relationship = "source did never exist"
)

func NewReceiver(opts ...ReceiverOption) *Receiver {
	queue := make(chan IncomingMention, 100) // @todo: configure buffer size
	receiver := &Receiver{
		schemes: []Scheme{
			"http",
			"https",
		},
		httpClient: http.DefaultClient,
		enqueue:    queue,
		dequeue:    queue,
		shutdown:   make(chan struct{}),
	}
	for _, opt := range opts {
		opt(receiver)
	}
	return receiver
}

func WithScheme(scheme ...Scheme) ReceiverOption {
	return func(r *Receiver) {
		r.schemes = append(r.schemes, scheme...)
	}
}

func WithListener(listener ...Listener) ReceiverOption {
	return func(r *Receiver) {
		r.listeners = append(r.listeners, listener...)
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

	if !receiver.IsSchemeSupported(Scheme(sourceURL.Scheme)) {
		return BadRequest("source url scheme not supported")
	}
	if !receiver.IsSchemeSupported(Scheme(targetURL.Scheme)) {
		return BadRequest("target url scheme not supported")
	}

	if !receiver.TargetExists(targetURL) {
		return BadRequest("target does not exist")
	}
	if !receiver.TargetAccepts(targetURL) {
		return BadRequest("target does not accept webmentions")
	}

	select {
	case receiver.enqueue <- IncomingMention{sourceURL, targetURL}:
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
	slog.Info("processing mention", "source", mention.Source.String(), "target", mention.Target.String())
	// @todo: log any mentions, even (especially) invalid ones
	rel, err := receiver.SourceToTargetRel(mention.Source, mention.Target)
	if err != nil {
		// @todo: log error
		return
	}
	if rel != SourceDoesNotExist && rel != SourceDoesNotLinkToTarget {
		// Processing should be idempotent
		for _, listener := range receiver.listeners {
			listener.Receive(mention, rel)
		}
	}
}

func (receiver *Receiver) TargetExists(target URL) bool {
	return false // @todo: implement / user provided
}

func (receiver *Receiver) TargetAccepts(target URL) bool {
	return false // @todo: implement / user provided
}

func (receiver *Receiver) IsSchemeSupported(scheme Scheme) bool {
	for _, other := range receiver.schemes {
		schemeLower := strings.ToLower(string(scheme))
		otherLower := strings.ToLower(string(other))
		if schemeLower == otherLower {
			return true
		}
	}
	return false
}

func (receiver *Receiver) SourceToTargetRel(source, target URL) (rel Relationship, err error) {
	resp, err := receiver.httpClient.Head(source.String())
	if err != nil {
		return SourceDoesNotExist, err
	}
	if resp.StatusCode == 410 {
		return SourceGotDeleted, nil
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// Fetch source to verify that it really links to the target (must have an exact match)
		//   - follow redirects, but limit it!
		//   - Accept header to indicate preferred content type
		//       - html: look for <a> <img> <video> etc.
		//       - json: look for properties whose values are an exact match
		//       - plain text: look for string match
		//       - 410 Gone: source was deleted

		// source used to link to target, still does
		return SourceUpdated, nil

		// source used to link to target, doesn't anymore
		return SourceRemovedTarget, nil

		// source didn't link to target, does now
		return SourceLinksToTarget, nil

		// source didn't link to target, still doesn't
		return SourceDoesNotLinkToTarget, nil
	}

	// @todo: 404 or other 4XX but we know it linked to target in the past?
	return SourceDoesNotExist, nil
}
