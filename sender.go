package webmention

import (
	"errors"
	"fmt"
	"net/url"
	"net/http"
	"net/html"
)

type (
	URL = *url.URL
	WebMentionSender interface {
		// Mention notifies the target url that it is being linked to by the source url.
		// Precondition: the source url must actually contain an exact match of the target url.
		Mention(source, target URL) error

		// Update resends any previously sent webmentions for the source url.
		Update(source URL) error
	}
	Sender struct {
		UserAgent string
		HttpClient http.Client
	}
	SenderOption func(*Sender)
)

// *Sender implements WebMentionSender
var _ WebMentionSender = (*Sender)(nil)

func NewSender(opts Option...) *Sender {
	sender := &Sender{
		UserAgent: "Webmention (github.com/cvanloo/gowebmention)"
		HttpClient: http.DefaultClient
	}
	for _, opt := range opts {
		opt(s)
	}
	return sender
}

// Use a custom user agent when sending web mentions.
// Should (but doesn't have to) include the string "Webmention" to give the
// receiver an indication as to the purpose of requests.
func WithUserAgent(agent string) SenderOption {
	return func(s *Sender) {
		s.UserAgent = agent
	}
}

func (sender *Sender) Mention(source, target URL) error {
	endpoint, err := sender.DiscoverEndpoint(target)

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

	return ErrNotImplemented
}

func (sender *Sender) Update(source URL) error {
	return ErrNotImplemented
}

func (sender *Sender) DiscoverEndpoint(target URL) (URL, error) {
	// 1. Fetch target url, follow redirects, to discover Webmention endpoint:
	//        - look for <link> or <a> element with rel=webmention

	{
		// @todo: HttpClient needs to follow redirects (the default client follows up to 10)
		//        Ensure that the client is actually configured correctly?
		resp, err := sender.HttpClient.Head(url.String())
		if err != nil {
			return nil, fmt.Errorf("endpoint discovery: cannot head target: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("endpoint discovery: head returned %s", resp.Status)
		}

		header := resp.Header()
		linkHeader := header.Get("Link")
		if linkHeader != "" {
			// @todo: resolve relative?
			// @todo: parse link header (needs to contain rel=webmention)
			endpoint, err := url.Parse(linkHeader)
			if err != nil {
				return nil, fmt.Errorf("endpoint discovery: invalid url in link header: %w", linkHeader)
			}
			return endpoint, nil
		}
	}

	{
		resp, err := sender.HttpClient.Get(url.String())
		if err != nil {
			return nil, fmt.Errorf("endpoint discovery: cannot get target: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("endpoint discovery: get returned %s", resp.Status)
		}

		// @todo: need to ensure resp.Body is valid utf-8
		doc, err := html.Parse(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("endpoint discovery: cannot parse html: %w", err)
		}
		var traverseHtml func(*html.Node)
		traverseHtml = func(node *html.Node) {
			if node.Type == html.ElementNode {
				if node.Data == "link" { // <link> has higher precedence than <a>
					hasRelWebMention := false
					href := ""
					for a := range node.Attr {
						// @todo: will also mach something like xxxwebmentionxxx, we need to make sure we only match
						//        the whole word webmention
						if a.Key == "rel" && strings.Contains(a.Val, "webmention") {
							hasRelWebMention = true
						} else if a.Key == "href" {
							href = a.Val
						}
					}
					if hasRelWebMention {
						endpoint, err := url.Parse(linkHeader)
						if err != nil {
							// @todo: instead of failing, try next <link>?
							return nil, fmt.Errorf("endpoint discovery: invalid url in link element: %w", linkHeader)
						}
						return endpoint, nil
					}
				} else if node.Data == "a" {
					// @todo: same as with <link>
				}
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling { // parse in depth-first order
				traverseHtml(child)
			}
		}
		parse(doc)
	}
}
