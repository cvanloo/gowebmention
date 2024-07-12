package webmention_test

import (
	"io"
	"fmt"
	"net/url"
	"testing"
	"net/http"
	"net/http/httptest"
	"sync"
	"log/slog"
	webmention "github.com/cvanloo/gowebmention"
)

func accepts(source, target *url.URL) bool {
	return true
}

func TestReceiveLocal(t *testing.T) {
	var ts *httptest.Server

	wg := sync.WaitGroup{}
	wg.Add(1) // either Done() in Report or in NotifierFunc

	webmention.Report = func(err error) {
		if err != nil {
			defer wg.Done()
			t.Fatal(err) // stop test
		}
	}

	receiver := webmention.NewReceiver(
		webmention.WithAcceptsFunc(accepts),
		webmention.WithNotifier(webmention.NotifierFunc(func(mention webmention.Mention) {
			defer wg.Done()
			slog.Info("notifier got called", "mention", mention)
			if mention.Status != webmention.StatusLink {
				t.Errorf("incorrect status, got: %s, want: %s", mention.Status, webmention.StatusLink)
			}
		})),
	)

	go receiver.ProcessMentions()

	mux := http.NewServeMux()
	mux.HandleFunc("/webmention", receiver.WebmentionEndpoint)
	mux.HandleFunc("/source/1", func(w http.ResponseWriter, r *http.Request) {
		body := fmt.Sprintf(`<p>Hello, <a href="%s">Target 1</a>!</p>`, ts.URL+"/target/1")
		w.Write([]byte(body))
	})
	ts = httptest.NewServer(mux)

	resp, err := http.DefaultClient.PostForm(ts.URL+"/webmention", map[string][]string{
		"source": {ts.URL+"/source/1"},
		"target": {ts.URL+"/target/1"},
	})
	if err != nil {
		t.Error(err)
		// continue
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("incorrect status code, got: %d, want: %d", resp.StatusCode, http.StatusAccepted)
		t.Logf("body: %s", must(io.ReadAll(resp.Body)))
	}

	wg.Wait()
}
