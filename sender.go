package webmention

import (
	"errors"
	"fmt"
	"net/url"
	"net/http"
	"net/html"

	"github.com/tomnomnom/linkheader"
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
	if err != nil {
		return fmt.Errorf("mention: %w", err)
	}

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

// DiscoverEndpoint searches the target for a webmention endpoint.
// Search stops at the first link that defines a webmention relationship.
// If that link is not a valid url, ErrInvalidRelWebmention is returned (check with errors.Is).
// If no link with a webmention relationship is found, ErrNoEndpointFound is returned.
// Any other error type indicates that we made a mistake, and not the target.
func (sender *Sender) DiscoverEndpoint(target URL) (URL, error) {
	{ // First make a HEAD request to look for a Link-Header
		// @todo: HttpClient needs to follow redirects (the default client follows up to 10)
		//        Ensure that the client is actually configured correctly?
		resp, err := sender.HttpClient.Head(url.String())
		if err != nil {
			return nil, fmt.Errorf("endpoint discovery: cannot head target: %w", err)
		}
		if resp.StatusCode < 200 && resp.StatusCode >= 300 {
			return nil, fmt.Errorf("endpoint discovery: head returned %s", resp.Status)
		}

		header := resp.Header()
		linkHeader := header.Get("Link")
		linkHeaderParts := strings.Split(linkHeader, ",")
		foundLink := ""
		if len(linkHeaderParts) == 1 {
			foundLink = linkHeaderParts[0]
		} else {
			for _, l := range linkheader.Parse(linkHeader) {
				if l.Rel == "webmention" {
					foundLink = l.URL
					break
				}
			}
		}
		if foundLink != "" { // Link header has highest priority [:rel-prio:]
			endpoint, err := url.Parse(linkHeader)
			if err != nil { // @todo: or continue on trying? [:should_we_continue_trying_or_not:]
				return nil, fmt.Errorf("endpoint discovery: %w: in link header: %w", ErrInvalidRelWebmention, foundLink)
			}
			return endpoint, nil
		}
	}

	{ // No Link header present, so request HTML content and scan it for <link> and <a> elements
		req, err := http.NewRequest(http.MethodGet, url.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("endpoint discovery: cannot create request from url: %s: because: %w", url, err)
		}
		req.Header.Set("Accept", "text/html")
		resp, err := sender.HttpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("endpoint discovery: cannot get target: %w", err)
		}
		if resp.StatusCode < 200 && resp.StatusCode >= 300 {
			return nil, fmt.Errorf("endpoint discovery: get returned %s", resp.Status)
		}

		// @todo: need to ensure resp.Body is valid utf-8
		doc, err := html.Parse(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("endpoint discovery: cannot parse html: %w", err)
		}
		var (
			traverseHtml func(*html.Node)
			firstLinkRel, firstARel URL
			traverseErr error
		)
		traverseHtml = func(node *html.Node) {
			if node.Type == html.ElementNode {
				if node.Data == "link" {
					url, err := scanForRelLink(node)
					if err != nil && !errors.Is(err, ErrNoRelWebmention) {
						traverseErr = err
						return
					}
					firstLinkRel = url
				} else if node.Data == "a" {
					url, err := scanForRelLink(node)
					if err != nil && !errors.Is(err, ErrNoRelWebmention) {
						traverseErr = err
						return
					}
					firstARel = url
				}
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling { // parse in depth-first order
				traverseHtml(child)
			}
		}
		traverseHtml(doc)
		if traverseErr != nil {
			return nil, fmt.Errorf("endpoint discovery: %w: in <link> or <a> element: %w", ErrInvalidRelWebmention, traverseErr)
		}
		if firstLinRel != nil { // <link> has higher precedence than <a> [:rel-prio:]
			return firstLinkRel, nil
		}
		if firstARel != nil {
			return firstARel, nil
		}
	}

	return nil, ErrNoEndpointFound
}

func scanForRelLink(node *html.Node) (URL, error) {
	hasRelVal := false
	href := ""
	for a := range node.Attr {
		// @todo: what if for some reason there are more than one rel="" in the same node?
		if !hasRelVal && a.Key == "rel" {
			relVals := strings.Split(a.Val, " ")
			for relVal := range relVals {
				if strings.ToLower(relVal) == "webmention" {
					hasRelVal = true
					break
				}
			}
		} else if a.Key == "href" {
			href = a.Val
		}
	}
	if hasRelVal {
		return url.Parse(href)
	}
	return nil, ErrNoRelWebmention
}
