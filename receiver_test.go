package webmention_test

import (
	//"io"
	//"fmt"
	"net/url"
	//"testing"
	//"net/http"
	//"net/http/httptest"

	//webmention "github.com/cvanloo/gowebmention"
)

func exists(target *url.URL) bool {
	switch target.Path {
	default:
		return false
	case "/target/1":
		return true
	}
}

func accepts(source, target *url.URL) bool {
	return true
}

/*
func TestReceiveLocal(t *testing.T) {
	var ts *httptest.Server
	var listenerCalled bool = false

	receiver := webmention.NewReceiver(
		webmention.WithExistsFunc(exists),
		webmention.WithAcceptsFunc(accepts),
		webmention.WithListener(webmention.ListenerFunc(func(mention webmention.IncomingMention, status webmention.Status) {
			listenerCalled = true
			t.Logf("listener: mention: %#v, status: %s", mention, status)
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

	// @todo: have to figure out how to make sure that the listener is called, or not.

	if !listenerCalled {
		t.Error("listener has not been called")
	}
}*/
