package webmention_test

import (
	"net/url"
	"testing"

	webmention "github.com/cvanloo/gowebmention"
)

type Targets []struct {
	Url      string
	Comment  string
	Expected string
}

var targets = Targets{
	{
		Url:      "https://webmention.rocks/test/1",
		Comment:  "HTTP Link header, unquoted rel, relative URL",
		Expected: "https://webmention.rocks/test/1/webmention?head=true",
	},
	{
		Url:      "https://webmention.rocks/test/2",
		Comment:  "HTTP Link header, unquoted rel, absolute URL",
		Expected: "https://webmention.rocks/test/2/webmention?head=true",
	},
	{
		Url:      "https://webmention.rocks/test/3",
		Comment:  "HTML <link> tag, relative URL",
		Expected: "https://webmention.rocks/test/3/webmention",
	},
	{
		Url:      "https://webmention.rocks/test/4",
		Comment:  "HTML <link> tag, absolute URL",
		Expected: "https://webmention.rocks/test/4/webmention",
	},
	{
		Url:      "https://webmention.rocks/test/5",
		Comment:  "HTML <a> tag, relative URL",
		Expected: "https://webmention.rocks/test/5/webmention",
	},
	{
		Url:      "https://webmention.rocks/test/6",
		Comment:  "HTML <a> tag, absolute URL ",
		Expected: "https://webmention.rocks/test/6/webmention",
	},
	{
		Url:      "https://webmention.rocks/test/7",
		Comment:  "HTTP Link header with strange casing",
		Expected: "https://webmention.rocks/test/7/webmention?head=true",
	},
	{
		Url:      "https://webmention.rocks/test/8",
		Comment:  "HTTP Link header, quoted rel",
		Expected: "https://webmention.rocks/test/8/webmention?head=true",
	},
	{
		Url:      "https://webmention.rocks/test/9",
		Comment:  "Multiple rel values on a <link> tag",
		Expected: "https://webmention.rocks/test/9/webmention",
	},
	{
		Url:      "https://webmention.rocks/test/10",
		Comment:  "Multiple rel values on a Link header",
		Expected: "https://webmention.rocks/test/10/webmention?head=true",
	},
	{
		Url:      "https://webmention.rocks/test/11",
		Comment:  "Multiple Webmention endpoints advertised: Link, <link>, <a> ",
		Expected: "https://webmention.rocks/test/11/webmention",
	},
	{
		Url:      "https://webmention.rocks/test/12",
		Comment:  "Checking for exact match of rel=webmention",
		Expected: "https://webmention.rocks/test/12/webmention",
	},
	{
		Url:      "https://webmention.rocks/test/13",
		Comment:  "False endpoint inside an HTML comment",
		Expected: "https://webmention.rocks/test/13/webmention",
	},
	{
		Url:      "https://webmention.rocks/test/14",
		Comment:  "False endpoint in escaped HTML",
		Expected: "https://webmention.rocks/test/14/webmention",
	},
	{
		Url:      "https://webmention.rocks/test/15",
		Comment:  "Webmention href is an empty string",
		Expected: "https://webmention.rocks/test/15",
	},
	{
		Url:      "https://webmention.rocks/test/16",
		Comment:  "Multiple Webmention endpoints advertised: <a>, <link>",
		Expected: "https://webmention.rocks/test/16/webmention",
	},
	{
		Url:      "https://webmention.rocks/test/17",
		Comment:  "Multiple Webmention endpoints advertised: <link>, <a>",
		Expected: "https://webmention.rocks/test/17/webmention",
	},
	{
		Url:      "https://webmention.rocks/test/18",
		Comment:  "Multiple HTTP Link headers",
		Expected: "https://webmention.rocks/test/18/webmention?head=true",
	},
	{
		Url:      "https://webmention.rocks/test/19",
		Comment:  "Single HTTP Link header with multiple values",
		Expected: "https://webmention.rocks/test/19/webmention?head=true",
	},
	{
		Url:      "https://webmention.rocks/test/20",
		Comment:  "Link tag with no href attribute",
		Expected: "https://webmention.rocks/test/20/webmention",
	},
	{
		Url:      "https://webmention.rocks/test/21",
		Comment:  "Webmention endpoint has query string parameters",
		Expected: "https://webmention.rocks/test/21/webmention?query=yes",
	},
	{
		Url:      "https://webmention.rocks/test/22",
		Comment:  "Webmention endpoint is relative to the path",
		Expected: "https://webmention.rocks/test/22/webmention",
	},
	{
		Url:     "https://webmention.rocks/test/23/page",
		Comment: "Webmention target is a redirect and the endpoint is relative",
		// No expected, because value changes with each test
	},
}

func TestEndpointDiscovery(t *testing.T) {
	sender := webmention.NewSender()

	for _, target := range targets {
		url, err := url.Parse(target.Url)
		if err != nil {
			t.Fatal(err)
		}
		endpoint, err := sender.DiscoverEndpoint(url)
		if err != nil {
			t.Log(target.Comment)
			t.Errorf("endpoint discovery failed for: %s with reason: %s", target.Url, err)
		} else if target.Expected != "" && endpoint.String() != target.Expected {
			t.Log(target.Comment)
			t.Errorf("endpoint discovery failed for: %s with reason: returned incorrect endpoint: %s, expected: %s", target.Url, endpoint, target.Expected)
		}
	}
}

func must[T any](t T, e error) T {
	if e != nil {
		panic(e)
	}
	return t
}

func TestMentioning(t *testing.T) {
	sender := webmention.NewSender()

	err := sender.Mention(must(url.Parse("http://localhost/")), must(url.Parse(targets[0].Url)))
	if err != nil {
		t.Errorf("mentioning failed for: %s with reason: %s", targets[0].Url, err)
	}
}
