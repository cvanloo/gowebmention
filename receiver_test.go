package webmention_test

import (
	"errors"
	"strconv"
	"io"
	"fmt"
	"net/url"
	"testing"
	"net/http"
	"net/http/httptest"
	"sync"
	"log"
	"context"
	"time"
	webmention "github.com/cvanloo/gowebmention"
)

func ExampleReceiver() {
	acceptForTargetUrl  := must(url.Parse("https://example.com"))
	webmentionee := webmention.NewReceiver(
		webmention.WithAcceptsFunc(func(source, target *url.URL) bool {
			return acceptForTargetUrl.Scheme == target.Scheme && acceptForTargetUrl.Host == target.Host
		}),
		webmention.WithNotifier(webmention.NotifierFunc(func(mention webmention.Mention) {
			fmt.Printf("received webmention from %s for %s, status %s", mention.Source, mention.Target, mention.Status)
		})),
	)
	mux := &http.ServeMux{}
	mux.Handle("/api/webmention", webmentionee)
	srv := http.Server{
		Addr: ":8080",
		Handler: mux,
	}

	// [!] should be started before http handler starts receiving
	// [!] You can start as many processing goroutines as you'd like
	go webmentionee.ProcessMentions()
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("http server error: %v", err)
		}
	}()

	// [!] Once it's time for shutdown...
	shutdownCtx, release := context.WithTimeout(context.Background(), 20*time.Second)
	defer release()
	srv.SetKeepAlivesEnabled(false)
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown error: %v", err)
	}
	// [!] shut down the receiver only after http endpoint stopped
	webmentionee.Shutdown(shutdownCtx)
}

func accepts(source, target *url.URL) bool {
	switch target.Path {
	default:
		return true
	case "/target/4":
		return false
	}
}

type TestCase struct {
	Comment string
	SourceHandler func(ts **httptest.Server) func(w http.ResponseWriter, r *http.Request)
	ExpectedHttpStatus int
	ExpectedMentionStatus webmention.Status
	ExpectedError error
}

var TestCases = []TestCase{
	{
		Comment: "source links to target",
		SourceHandler: func(ts **httptest.Server) func(w http.ResponseWriter, r *http.Request) {
			return func(w http.ResponseWriter, r *http.Request) {
				body := fmt.Sprintf(`<p>Hello, <a href="%s">Target 1</a>!</p>`, (*ts).URL+"/target/1")
				w.Write([]byte(body))
			}
		},
		ExpectedHttpStatus: 202,
		ExpectedMentionStatus: webmention.StatusLink,
	},
	{
		Comment: "source does not link to target",
		SourceHandler: func(**httptest.Server) func(w http.ResponseWriter, r *http.Request) {
			return func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("<p>I'm not linking to anything. ;-(</p>"))
			}
		},
		ExpectedHttpStatus: 202, // this type of validation happens async
		ExpectedMentionStatus: webmention.StatusNoLink,
	},
	{
		Comment: "source was deleted",
		SourceHandler: func(**httptest.Server) func(w http.ResponseWriter, r *http.Request) {
			return func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusGone)
				w.Write([]byte(`<p>This post was deleted</p>`))
			}
		},
		ExpectedHttpStatus: 202,
		ExpectedMentionStatus: webmention.StatusDeleted,
	},
	{
		Comment: "target does not exist or accept webmentions",
		SourceHandler: func(ts **httptest.Server) func(w http.ResponseWriter, r *http.Request) {
			return func(w http.ResponseWriter, r *http.Request) {
				body := fmt.Sprintf(`<p>Hello, <a href="%s">Target 4</a>!</p>`, (*ts).URL+"/target/4")
				w.Write([]byte(body))
			}
		},
		ExpectedHttpStatus: 400,
		//ExpectedMentionStatus
	},
	{
		Comment: "source does not exist",
		SourceHandler: func(**httptest.Server) func(w http.ResponseWriter, r *http.Request) {
			return func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte("Resource not found"))
			}
		},
		ExpectedHttpStatus: 202,
		//ExpectedMentionStatus
		ExpectedError: webmention.ErrSourceNotFound,
	},
}

func TestReceiveLocal(t *testing.T) {
	var ts *httptest.Server

	wg := sync.WaitGroup{}
	wg.Add(len(TestCases)) // either Done() in Report or in NotifierFunc, or in error cases

	webmention.Report = func(err error, mention webmention.Mention) {
		if err != nil {
			defer wg.Done()
			testNumber := must(strconv.Atoi(string(mention.Source.Path[len("/source/"):])))
			testCase := TestCases[testNumber-1]

			if testCase.ExpectedError == nil || !errors.Is(err, testCase.ExpectedError) {
				t.Errorf("incorrect error: got: %s, want: %s", err, testCase.ExpectedError)
			}
		}
	}

	receiver := webmention.NewReceiver(
		webmention.WithAcceptsFunc(accepts),
		webmention.WithNotifier(webmention.NotifierFunc(func(mention webmention.Mention) {
			defer wg.Done()
			testNumber := must(strconv.Atoi(string(mention.Source.Path[len("/source/"):])))
			testCase := TestCases[testNumber-1]
			if mention.Status != testCase.ExpectedMentionStatus {
				t.Errorf("incorrect status, got: %s, want: %s", mention.Status, testCase.ExpectedMentionStatus)
			}
		})),
	)

	go receiver.ProcessMentions()

	mux := http.NewServeMux()
	mux.Handle("/webmention", receiver)

	for i, testCase := range TestCases {
		mux.HandleFunc(fmt.Sprintf("/source/%d", i+1), testCase.SourceHandler(&ts))
	}

	ts = httptest.NewServer(mux)

	for i, testCase := range TestCases {
		t.Logf("test case: %d: %s", i+1, testCase.Comment)
		resp, err := http.DefaultClient.PostForm(ts.URL+"/webmention", map[string][]string{
			"source": {fmt.Sprintf("%s/source/%d", ts.URL, i+1)},
			"target": {fmt.Sprintf("%s/target/%d", ts.URL, i+1)},
		})
		if err != nil {
			wg.Done()
			t.Error(err)
			continue
		}
		if resp.StatusCode != testCase.ExpectedHttpStatus {
			wg.Done()
			t.Errorf("incorrect status code, got: %d, want: %d", resp.StatusCode, testCase.ExpectedHttpStatus)
			t.Logf("body: %s", must(io.ReadAll(resp.Body)))
		}
		if testCase.ExpectedHttpStatus != http.StatusAccepted {
			wg.Done() // because listener or report wont ever be invoked
		}
	}

	wg.Wait()
}
