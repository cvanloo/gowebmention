package webmention

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/tomnomnom/linkheader"
	"golang.org/x/net/html"
)

type (
	URL              = *url.URL
	WebMentionSender interface {
		// Mention notifies the target url that it is being linked to by the source url.
		// Precondition: the source url must actually contain an exact match of the target url.
		Mention(source, target URL) error

		// Calls Mention for each of the target urls.
		// All mentions are made from the same source.
		// Continues on on errors with the next target.
		// The returned error is a composite consisting of all encountered errors.
		MentionMany(source, targets []URL) error

		// Update resends any previously sent webmentions for the source url.
		// The current set of targets on the source is used to find new mentions and send them notifications accordingly.
		// If the source url has been deleted, it is expected (of the user) to
		// have it setup to return 410 Gone and return a tombstone
		// representation in the body.
		Update(source URL, targets []URL) error
	}
	Persister interface {
		// PastTargets compiles a list of all the targets that the source linked to on the last update.
		PastTargets(source URL) ([]URL, error)
	}
	Sender struct {
		UserAgent  string
		HttpClient *http.Client
		Persist Persister
	}
	SenderOption func(*Sender)
)

// *Sender implements WebMentionSender
var _ WebMentionSender = (*Sender)(nil)

func NewSender(opts ...SenderOption) *Sender {
	sender := &Sender{
		UserAgent:  "Webmention (github.com/cvanloo/gowebmention)",
		HttpClient: http.DefaultClient,
		Persist: &XmlPersiter{
			Path: ".",
		},
	}
	for _, opt := range opts {
		opt(sender)
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

// Use custom persistent store.
// Per default data is persisted to an XML file in the process working directory.
func WithPersist(persist Persister) SenderOption {
	return func(s *Sender) {
		s.Persist = persist
	}
}

func (sender *Sender) Mention(source, target URL) error {
	endpoint, err := sender.DiscoverEndpoint(target)
	if err != nil {
		return fmt.Errorf("mention: %w", err)
	}

	log := slog.With(
		"function", "Mention",
		slog.Group("request_info",
			"source", source.String(),
			"target", target.String(),
		),
	)

	resp, err := sender.HttpClient.PostForm(endpoint.String(), url.Values{
		"source": {source.String()},
		"target": {target.String()},
	})
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		log.Error(
			"post request failed",
			"status", resp.Status,
			"body", string(body),
		)
		return fmt.Errorf("mention: endpoint: %s: post form returned: %s", endpoint, resp.Status)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		log.Info("request was processed synchronously",
			"endpoint", endpoint,
		)
	case http.StatusCreated:
		log.Info("request is being processed asynchronously",
			"endpoint", endpoint,
			"status_page", resp.Header.Values("Location"),
		)
	case http.StatusAccepted:
		log.Info("request is being processed asynchronously",
			"endpoint", endpoint,
			"status_page", nil,
		)
	}

	return nil
}

func (sender *Sender) MentionMany(source, targets []URL) (err error) {
	for _, target := range targets {
		merr := sender.Mention(source, target)
		err = errors.Join(err, merr)
	}
	return err
}

func (sender *Sender) Update(source URL, currentTargets []URL) error {
	pastTargets, err := sender.PastTargets(source)
	if err != nil {
		return fmt.Errorf("update: cannot get past targets for: %s: %w", source, err)
	}
	targets := make([]URL, 0, len(pastTargets) + len(targets))
	for target := range pastTargets {
		targets = append(targets, target)
	}
	for _, maybeNewTarget := range currentTargets {
		if _, isOld := pastTargets[maybeNewTarget]; !isOld {
			targets = append(targets, maybeNewTarget)
		}
	}

	return sender.MentionMany(targets)
}

// DiscoverEndpoint searches the target for a webmention endpoint.
// Search stops at the first link that defines a webmention relationship.
// If that link is not a valid url, ErrInvalidRelWebmention is returned (check with errors.Is).
// If no link with a webmention relationship is found, ErrNoEndpointFound is returned.
// Any other error type indicates that we made a mistake, and not the target.
func (sender *Sender) DiscoverEndpoint(target URL) (endpoint URL, err error) {
	{ // First make a HEAD request to look for a Link-Header
		// @todo: HttpClient needs to follow redirects (the default client follows up to 10)
		//        Ensure that the client is actually configured correctly?
		resp, err := sender.HttpClient.Head(target.String())
		{
			// go doc http.Do: body needs to be read to EOF and closed [:read_eof_and_close_body:]
			bs, rerr := io.ReadAll(resp.Body)
			defer func() {
				var errTooMuch error
				if len(bs) != 0 {
					errTooMuch = fmt.Errorf("endpoint discovery: expected only tip but got whole shaft: %d bytes read from response body", len(bs))
				}
				err = errors.Join(err, rerr, errTooMuch)
			}()
		}
		if err != nil {
			return nil, fmt.Errorf("endpoint discovery: cannot head target: %w", err)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("endpoint discovery: head returned %s", resp.Status)
		}

		linkHeaders := resp.Header.Values("Link")
		var foundLink string
		for _, l := range linkheader.ParseMultiple(linkHeaders) {
			relVals := strings.Split(l.Rel, " ")
			for _, relVal := range relVals {
				if strings.ToLower(relVal) == "webmention" {
					foundLink = l.URL
					break
				}
			}
		}
		if foundLink != "" { // Link header takes precedence before <link> and <a>
			endpoint, err := url.Parse(foundLink)
			if err != nil { // @todo: or continue on trying? [:should_we_continue_trying_or_not:]
				return nil, fmt.Errorf("endpoint discovery: %w: in link header: %w", ErrInvalidRelWebmention, err)
			}
			return target.ResolveReference(endpoint), nil
		}
	}

	{ // No Link header present, so request HTML content and scan it for <link> and <a> elements
		req, err := http.NewRequest(http.MethodGet, target.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("endpoint discovery: cannot create request from url: %s: because: %w", target, err)
		}
		req.Header.Set("Accept", "text/html")
		resp, err := sender.HttpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("endpoint discovery: cannot get target: %w", err)
		}
		defer func() {
			// go doc http.Do: body needs to be read to EOF and closed [:read_eof_and_close_body:]
			// parser below will read body till EOF
			cerr := resp.Body.Close()
			if cerr != nil {
				err = errors.Join(err, cerr)
			}
		}()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("endpoint discovery: get returned %s", resp.Status)
		}

		// @todo: need to ensure resp.Body is valid utf-8
		doc, err := html.Parse(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("endpoint discovery: cannot parse html: %w", err)
		}
		var (
			traverseHtml            func(*html.Node) bool
			firstLinkRel, firstARel URL
			traverseErr             error
		)
		traverseHtml = func(node *html.Node) bool {
			if node.Type == html.ElementNode {
				if node.Data == "link" {
					url, err := scanForRelLink(node)
					if err != nil {
						if !errors.Is(err, ErrNoRelWebmention) {
							traverseErr = err
							return false
						}
					} else {
						firstLinkRel = url
						return false
					}
				} else if node.Data == "a" {
					url, err := scanForRelLink(node)
					if err != nil {
						if !errors.Is(err, ErrNoRelWebmention) {
							traverseErr = err
							return false
						}
					} else {
						firstARel = url
						return false
					}
				}
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling { // parse in depth-first order
				if !traverseHtml(child) {
					return false
				}
			}
			return true
		}
		traverseHtml(doc)
		if traverseErr != nil {
			return nil, fmt.Errorf("endpoint discovery: %w: in <link> or <a> element: %w", ErrInvalidRelWebmention, traverseErr)
		}
		if firstLinkRel != nil {
			return target.ResolveReference(firstLinkRel), nil
		}
		if firstARel != nil {
			return target.ResolveReference(firstARel), nil
		}
	}

	return nil, ErrNoEndpointFound
}

func scanForRelLink(node *html.Node) (URL, error) {
	hasRelVal := false
	hasHrefVal := false
	href := ""
	for _, a := range node.Attr {
		// @todo: what if for some reason there are more than one rel="" in the same node?
		if !hasRelVal && a.Key == "rel" {
			relVals := strings.Split(a.Val, " ")
			for _, relVal := range relVals {
				if strings.ToLower(relVal) == "webmention" {
					hasRelVal = true
					break
				}
			}
		} else if a.Key == "href" {
			hasHrefVal = true
			href = a.Val
		}
	}
	if hasRelVal && hasHrefVal {
		return url.Parse(href)
	}
	return nil, ErrNoRelWebmention
}

func (sender *Sender) PastTargets(source URL) (pastTargets map[URL]struct{}, err error) {
	targets, err := sender.Persist.PastTargets(source)
	if err != nil {
		return fmt.Errorf("past targets: %w", err)
	}
	for _, target := range targets {
		pastTargets[target] = struct{}{}
	}
	return pastTargets, nil
}
