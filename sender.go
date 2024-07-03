package webmention

import "net/url"

type (
	Sender struct {
		UserAgent string
	}
	SenderOption func(*Sender)
)

func NewSender(opts Option...) *Sender {
	sender := &Sender{
		UserAgent: "Webmention (github.com/cvanloo/gowebmention)"
	}
	for _, opt := range opts {
		opt(s)
	}
	return sender
}

func WithUserAgent(agent string) SenderOption {
	return func(s *Sender) {
		s.UserAgent = agent
	}
}

func (sender *Sender) Mention(source net.URL, target net.URL) error {
	// 1. Fetch target url, follow redirects, to discover Webmention endpoint:
	//    - Check for HTTP 'Link' header with rel=webmention
	//    - if document content type is HTML
	//        - look for <link> or <a> element with rel=webmention
	//    (precedence: first Link header, first <link>, first <a>)
	//    - MAY make HEAD request first to check for Link header

	// 2. Resolve endpoint url relative to target url (only if endpoint url is relative)
	//    Query string params must be preserved as such and not sent as (POST) body parameters

	// 3. Notify receiver on its endpoint
	//      - POST endpoint (preserve query string params, don't put them into the POST body!)
	//      - x-www-form-urlencoded:
	//         - source: sender's page containing link
	//         - target: url of page being linked to

	// 4. Check response
	//      - any 2XX considered success
	//      - 200 OK: Request has been processed (synchronously)
	//      - 201 Created: Request will be processed async, check Location header field for status page
	//      - 202 Accepted: Request is will be processed async, no way to check status


	// If source url updated:
	//   - rediscover endpoint (in case it changed)
	//   - resend any previously sent webmentions (including if the target has been removed from the page)
	//   - SHOULD send webmentions for any new links appearing in the source
	// Including if response to source is shown on the source as comment, that is also an update to the source url
	//   resend any previously sent webmentions, (but probably shouldn't send to response -> loop?)

	// If source url deleted:
	//   - Need to return 410 Gone for the url
	//   - Show tombstone representation of deleted post
	//   - resend any previously sent webmentions for the post
}
