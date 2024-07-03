package webmention

import "net/url"

type (
	Receiver struct {
		Schemes []Scheme
	}
	Scheme string // @todo: does net/url already have something like this?
	ReceiverOption func(*Receiver)
)

func NewReceiver(opts ...ReceiverOption) *Receiver {
	receiver := &Receiver{
		Schemes: []Scheme{
			"http",
			"https",
		},
	}
	for _, opt := range opts {
		opt(receiver)
	}
	return receiver
}

func WithScheme(scheme Scheme) ReceiverOption {
	return func(r *Receiver) {
		r.Schemes = append(r.Schemes, scheme)
	}
}

func (receiver *Receiver) Receive(source, target url.URL) error {
	// Processing should be idempotent

	// 1. Verify source and target urls (todo)
	// 2. Queue and process request async (202 Accepted, no Location)
	//    Return human readable body (maybe)


	// Update existing webmentions (has received same source and target in past)
	// verify ...
	// update any existing data picked up from source
	// source returns 410 gone or 200 OK but does not have a source link anymore:
	//   - remove the existing webmention or mark it as deleted
	return ErrNotImplemented
}

func (receiver *Receiver) Verify(url url.URL) bool {
	// Sync: (Request verification)
	// ! target url malformed
	// ! target url cannot be found
	// ! target url does not accept webmentions
	// scheme supported (http, https)
	// source == target -> reject (handle this in the request handler)
	// target must be valid resource for which we can accept webmentions (check this synchronously to return 400 Bad Request instead of 202)

	// Async: (Webmention verification)
	// - ! source url cannot be found
	// - ! source url does not link to target url
	// Fetch source to verify that it really links to the target (must have an exact match)
	//   - follow redirects, but limit it!
	//   - Accept header to indicate preferred content type
	//       - html: look for <a> <img> <video> etc.
	//       - json: look for properties whose values are an exact match
	//       - plain text: look for string match

	// Verification failure:
	// Return 400 Bad Request
	// Optionally include description of error in body

	// Failure on our side (receiver):
	// return 500 Internal Server Error

	// Verification successful: 
	// May display content from the source on the target page or other pages along any other data picked up from source
	// Notify receiver (author of source) via Matrix/Discord bot?
	return false
}
