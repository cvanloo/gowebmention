// Provides a service that listens for and processes incoming Webmentions.
// After some preliminary (synchronous) validation, Webmention requests are
// queued and then processed asynchronously.
//
// This application can be run (for example) as a daemon.
// However, it is intended more as an example of how to use this library in
// your own project.
//
// By registering listeners you can write your own logic to react to Webmentions:
//
//	receiver := webmention.NewReceiver(
//	   webmention.WithNotifier(customHandler),
//	)
//
// ...where customHandler implements the webmention.Notifier interface.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	webmention "github.com/cvanloo/gowebmention"
)

func init() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
}

const shutdownTimeout = 20 * time.Second

func main() {
	receiver := webmention.NewReceiver(
		webmention.WithAcceptsFunc(func(source, target *url.URL) bool {
			return true
		}),
		webmention.WithNotifier(webmention.NotifierFunc(func(mention webmention.Mention) {
			slog.Info("received webmention",
				"source", mention.Source.String(),
				"target", mention.Target.String(),
				"status", mention.Status,
			)
		})),
	)

	go receiver.ProcessMentions()

	mux := &http.ServeMux{}
	mux.HandleFunc("/webmention", receiver.WebmentionEndpoint)

	server := http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	go func() {
		err := server.ListenAndServe()
		//err := server.ListenAndServeTLS()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error(fmt.Sprintf("http server error: %s", err))
		}
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)
	<-c // wait for interrupt
	slog.Info("interrupt received, shutting down")

	shutdownCtx, shutdownRelease := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownRelease()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error(fmt.Sprintf("http shutdown error: %s", err))
	}
	receiver.Shutdown(shutdownCtx)
}
