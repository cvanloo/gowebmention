package webmention_test

import (
	"fmt"
	"net/url"
	"testing"
	"net/http"
	"net/http/httptest"

	webmention "github.com/cvanloo/gowebmention"
)

type Targets []struct {
	Url      string
	Comment  string
	Expected string
	SourceHandler func(ts **httptest.Server) http.HandlerFunc
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

func must[T any](t T, e error) T {
	if e != nil {
		panic(e)
	}
	return t
}

func TestEndpointDiscoveryRocks(t *testing.T) {
	sender := webmention.NewSender()

	for _, target := range targets {
		url := must(url.Parse(target.Url))
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

func TestMentioningRocks(t *testing.T) {
	sender := webmention.NewSender()

	source := must(url.Parse("http://blog.vanloo.ch/test/webmention.html"))
	for _, target := range targets {
		err := sender.Mention(source, must(url.Parse(target.Url)))
		if err != nil {
			t.Errorf("mentioning failed for: %s with reason: %s", target.Url, err)
		}
		//break
	}
}

func TestMentioningUpdatesRocks(t *testing.T) {
}

func TestMentioningDeletesRocks(t *testing.T) {
}

var localTargets = Targets{
	{
		Url: "/test/1",
		Comment: "HTTP Link header, unquoted rel, relative URL",
		Expected: "/test/1/webmention?head=true",
		SourceHandler: func(ts **httptest.Server) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				h := w.Header()
				h.Add("Link", "</test/1/webmention?head=true>; rel=webmention")
				w.WriteHeader(http.StatusOK)
			}
		},
	},
	{
		Url: "/test/2",
		Comment: "HTTP Link header, unquoted rel, absolute URL",
		Expected: "/test/2/webmention?head=true",
		SourceHandler: func(ts **httptest.Server) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				h := w.Header()
				h.Add("Link", "</wrong/link>; rel=\"whatever\"")
				h.Add("Link", fmt.Sprintf("<%s/test/2/webmention?head=true>; rel=webmention", (*ts).URL))
				w.WriteHeader(http.StatusOK)
			}
		},
	},
	{
		Url: "/test/3",
		Comment: "HTML <link> tag, relative URL",
		Expected: "/test/3/webmention",
		SourceHandler: func(ts **httptest.Server) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(
					`<!DOCTYPE html>
					<html lang="en">
					<head>
					<link rel="stylesheet" href="styles.css">
					<link rel="webmention" href="/test/3/webmention"
					</head>
					<body>
					<h1>This is a test page.</h1>
					<p>This is a test paragraph.</p>
					</body>
					</html>`,
				))
			}
		},
	},
	{
		Url: "/test/4",
		Comment: "HTML <link> tag, absolute URL",
		Expected: "/test/4/webmention",
		SourceHandler: func(ts **httptest.Server) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(fmt.Sprintf(
					`<!DOCTYPE html>
					<html lang="en">
					<head>
					<link rel="stylesheet" href="styles.css">
					<link rel="webmention" href="%s/test/4/webmention"
					</head>
					<body>
					<h1>This is a test page.</h1>
					<p>This is a test paragraph.</p>
					</body>
					</html>`,
					(*ts).URL,
				)))
			}
		},
	},
	{
		Url: "/test/5",
		Comment: "HTML <a> tag, relative URL",
		Expected: "/test/5/webmention",
		SourceHandler: func(ts **httptest.Server) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(fmt.Sprintf(
					`<!DOCTYPE html>
					<html lang="en">
					<head>
					<link rel="stylesheet" href="styles.css">
					</head>
					<body>
					<h1>This is a test page.</h1>
					<p>
					This is a test paragraph.
					You can find the webmention endpoint <a href="/test/5/webmention" rel="webmention">here</a>
					</p>
					</body>
					</html>`,
				)))
			}
		},
	},
	{
		Url: "/test/6",
		Comment: "HTML <a> tag, absolute URL",
		Expected: "/test/6/webmention",
		SourceHandler: func(ts **httptest.Server) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(fmt.Sprintf(
					`<!DOCTYPE html>
					<html lang="en">
					<head>
					<link rel="stylesheet" href="styles.css">
					</head>
					<body>
					<h1>This is a test page.</h1>
					<p>
					This is a test paragraph.
					You can find the webmention endpoint <a href="%s/test/6/webmention" rel="webmention">here</a>
					</p>
					</body>
					</html>`,
					(*ts).URL,
				)))
			}
		},
	},
	{
		Url: "/test/7",
		Comment: "HTTP Link header with strange casing",
		Expected: "/test/7/webmention?head=true",
		SourceHandler: func(ts **httptest.Server) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				h := w.Header()
				h.Add("LinK", "</test/7/webmention?head=true>; rel=webmention")
				w.WriteHeader(http.StatusOK)
			}
		},
	},
	{
		Url: "/test/8",
		Comment: "HTTP Link header, quoted rel",
		Expected: "/test/8/webmention?head=true",
		SourceHandler: func(ts **httptest.Server) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				h := w.Header()
				h.Add("Link", "</test/8/webmention?head=true>; rel=\"webmention\"")
				w.WriteHeader(http.StatusOK)
			}
		},
	},
	{
		Url: "/test/9",
		Comment: "Multiple rel values on a <link> tag",
		Expected: "/test/9",
		SourceHandler: func(ts **httptest.Server) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(fmt.Sprintf(
					`<!DOCTYPE html>
					<html lang="en">
					<head>
					<link rel="stylesheet" href="styles.css">
					<link rel="something webmention" href="%s/test/9"
					</head>
					<body>
					<h1>This is a test page.</h1>
					<p>This is a test paragraph.</p>
					</body>
					</html>`,
					(*ts).URL,
				)))
			}
		},
	},
	{
		Url: "/test/10",
		Comment: "Multiple rel values on a Link header",
		Expected: "/test/10/webmention?head=true",
		SourceHandler: func(ts **httptest.Server) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				h := w.Header()
				h.Add("Link", "</test/10/webmention?head=true>; rel=\"somethingelse webmention\"")
				w.WriteHeader(http.StatusOK)
			}
		},
	},
	{
		Url: "/test/11",
		Comment: "Multiple rel values on a Link header",
		Expected: "/test/11/webmention?head=true",
		SourceHandler: func(ts **httptest.Server) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				h := w.Header()
				h.Add("Link", "</test/11/webmention?head=true>; rel=webmention")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(fmt.Sprintf(
					`<!DOCTYPE html>
					<html lang="en">
					<head>
					<link rel="stylesheet" href="styles.css">
					<link rel="webmention" href="/test/11/wrong"
					</head>
					<body>
					<h1>This is a test page.</h1>
					<p>
					This is a test paragraph.
					You can find the webmention endpoint <a href="/test/11/alsowrong" rel="webmention">here</a>
					</p>
					</body>
					</html>`,
				)))
			}
		},
	},
	{
		Url: "/test/12",
		Comment: "Multiple rel values on a Link header",
		Expected: "/test/12/webmention",
		SourceHandler: func(ts **httptest.Server) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(fmt.Sprintf(
					`<!DOCTYPE html>
					<html lang="en">
					<head>
					<link rel="stylesheet" href="styles.css">
					<link rel="not-webmention" href="/test/12/wrong"
					</head>
					<body>
					<h1>This is a test page.</h1>
					<p>
					This is a test paragraph.
					You can find the webmention endpoint <a href="/test/12/webmention" rel="webmention">here</a>
					</p>
					</body>
					</html>`,
				)))
			}
		},
	},
	{
		Url: "/test/13",
		Comment: "False endpoint inside an HTML comment",
		Expected: "/test/13/webmention",
		SourceHandler: func(ts **httptest.Server) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(fmt.Sprintf(
					`<!DOCTYPE html>
					<html lang="en">
					<head>
					<link rel="stylesheet" href="styles.css">
					</head>
					<body>
					<h1>This is a test page.</h1>
					<p>
					This is a test paragraph.
					There is a comment here <!-- <a href="/test/13/wrong" rel="webmention">here</a> --> that contains a rel=webmention element.
					It should be ignored, since it's a comment, dummy!
					</p>
					<p>
					Here there is the <a href="/test/13/webmention" rel="webmention">correct</a> endpoint.
					</p>
					</body>
					</html>`,
				)))
			}
		},
	},
	{
		Url: "/test/14",
		Comment: "False endpoint in escaped HTML",
		Expected: "/test/14/webmention",
		SourceHandler: func(ts **httptest.Server) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(fmt.Sprintf(
					`<!DOCTYPE html>
					<html lang="en">
					<head>
					<link rel="stylesheet" href="styles.css">
					</head>
					<body>
					<h1>This is a test page.</h1>
					<p>
					Wrong endpoint in escaped html: <code>&lt;a href="/test/14/webmention/error" rel="webmention"&gt;&lt;/a&gt;</code>.
					Correct endpoint <a href="/test/14/webmention" rel="webmention">here</a>.
					</p>
					</body>
					</html>`,
				)))
			}
		},
	},
	{
		Url: "/test/15",
		Comment: "Webmention href is an empty string",
		Expected: "/test/15",
		SourceHandler: func(ts **httptest.Server) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(fmt.Sprintf(
					`<!DOCTYPE html>
					<html lang="en">
					<head>
					<link rel="stylesheet" href="styles.css">
					<link href="" rel="webmention">
					</head>
					<body>
					<h1>This is a test page.</h1>
					<p>
					Nobody is going to read through these test cases anyway, so I can put my secrets here:
					<marquee>I have eleven toes!</marquee>
					</p>
					</body>
					</html>`,
				)))
			}
		},
	},
	{
		Url: "/test/16",
		Comment: "Multiple Webmention endpoints advertised: <a>, <link>",
		Expected: "/test/16/webmention",
		SourceHandler: func(ts **httptest.Server) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(fmt.Sprintf(
					`<!DOCTYPE html>
					<html lang="en">
					<head>
					<link rel="stylesheet" href="styles.css">
					</head>
					<body>
					<h1>This is a test page.</h1>
					<p>
					The first endpoint in the &lt;a&gt; tag: <a href="/test/16/webmention" rel="webmention">here</a>.
					</p>
					<link href="/test/16/webmention/error" rel="webmention">
					</body>
					</html>`,
				)))
			}
		},
	},
	{
		Url: "/test/17",
		Comment: "Multiple Webmention endpoints advertised: <link>, <a>",
		Expected: "/test/17/webmention",
		SourceHandler: func(ts **httptest.Server) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(fmt.Sprintf(
					`<!DOCTYPE html>
					<html lang="en">
					<head>
					<link rel="stylesheet" href="styles.css">
					</head>
					<body>
					<h1>This is a test page.</h1>
					<p>
					Somewhere there is a link tag:
					<link href="/test/17/webmention" rel="webmention">
					And after that, there is an &lt;a&gt; tag: <a href="/test/16/webmention/error" rel="webmention">here</a>.
					</p>
					</body>
					</html>`,
				)))
			}
		},
	},
	{
		Url: "/test/18",
		Comment: "Multiple HTTP Link headers",
		Expected: "/test/18/webmention?head=true",
		SourceHandler: func(ts **httptest.Server) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				h := w.Header()
				h.Add("Link", "</test/whatever?head=true>; rel=whatever")
				h.Add("Link", "</test/18/webmention?head=true>; rel=\"somethingelse webmention\"")
				w.WriteHeader(http.StatusOK)
			}
		},
	},
	{
		Url: "/test/19",
		Comment: "Single HTTP Link header with multiple values",
		Expected: "/test/19/webmention?head=true",
		SourceHandler: func(ts **httptest.Server) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				h := w.Header()
				h.Add("Link", "</test/19/wrong>; rel=\"other\", </test/19/webmention?head=true>; rel=\"webmention\"")
				w.WriteHeader(http.StatusOK)
			}
		},
	},
	{
		Url: "/test/20",
		Comment: "Link tag with no href attribute",
		Expected: "/test/20/webmention",
		SourceHandler: func(ts **httptest.Server) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(fmt.Sprintf(
					`<!DOCTYPE html>
					<html lang="en">
					<head>
					<link rel="stylesheet" href="styles.css">
					</head>
					<body>
					<h1>This is a test page.</h1>
					<p>
					There is a link that missing its href attribute:
					<link rel="webmention">
					Instead use this endpoint: <a href="/test/20/webmention" rel="webmention">here</a>.
					</p>
					</body>
					</html>`,
				)))
			}
		},
	},
	{
		Url: "/test/21",
		Comment: "Webmention endpoint has query string parameters",
		Expected: "/test/21/webmention?query=yes",
		SourceHandler: func(ts **httptest.Server) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(fmt.Sprintf(
					`<!DOCTYPE html>
					<html lang="en">
					<head>
					<link rel="stylesheet" href="styles.css">
					<link rel="webmention" href="/test/21/webmention?query=yes">
					</head>
					<body>
					<h1>This is a test page.</h1>
					<p>
					This is test content.
					</p>
					</body>
					</html>`,
				)))
			}
		},
	},
	{
		Url: "/test/22",
		Comment: "Webmention endpoint is relative to the path",
		Expected: "/test/22/webmention",
		SourceHandler: func(ts **httptest.Server) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(fmt.Sprintf(
					`<!DOCTYPE html>
					<html lang="en">
					<head>
					<link rel="stylesheet" href="styles.css">
					<link rel="webmention" href="22/webmention">
					</head>
					<body>
					<h1>This is a test page.</h1>
					<p>
					This is test content.
					</p>
					</body>
					</html>`,
				)))
			}
		},
	},
	{
		Url: "/test/23",
		Comment: "Webmention target is a redirect and the endpoint is relative",
		Expected: "/redirect/endpoint/webmention",
		SourceHandler: func(ts **httptest.Server) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				h := w.Header()
				h.Set("Location", "/redirect")
				w.WriteHeader(http.StatusFound)
			}
		},
	},
}

func TestEndpointDiscoveryLocal(t *testing.T) {
	sender := webmention.NewSender()

	var ts *httptest.Server

	mux := http.NewServeMux()
	for _, target := range localTargets {
		// ok this is kinda cursed, the first time I've ever used a double pointer in Go
		// (feels like I'm doing something wrong)
		mux.HandleFunc(target.Url, target.SourceHandler(&ts))
	}
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(
			`<!DOCTYPE html>
			<html lang="en">
			<head>
			<link rel="stylesheet" href="styles.css">
			</head>
			<body>
			<h1>You probably came here through a redirect.</h1>
			<p>
			Here is your endpoint: <a href="/redirect/endpoint/webmention" rel="webmention">webmention</a>.
			</p>
			</body>
			</html>`,
		))
	})
	ts = httptest.NewServer(mux)
	defer ts.Close()

	for _, target := range localTargets {
		url := must(url.Parse(ts.URL+target.Url))
		expectedUrl := must(url.Parse(ts.URL+target.Expected))
		endpoint, err := sender.DiscoverEndpoint(url)
		if err != nil {
			t.Log(target.Comment)
			t.Errorf("endpoint discovery failed for: %s with reason: %s", url.String(), err)
		} else if target.Expected != "" && endpoint.String() != expectedUrl.String() {
			t.Log(target.Comment)
			t.Errorf("endpoint discovery failed for: %s with reason: returned incorrect endpoint: %s, expected: %s", url.String(), endpoint, expectedUrl.String())
		}
	}
}

func TestMentioningLocal(t *testing.T) {
}
