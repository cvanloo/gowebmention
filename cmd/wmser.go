package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	webmention "github.com/cvanloo/gowebmention"
)

func main() {
	receiver := webmention.NewReceiver()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // @todo: actually wait for goroutine to exit
	go receiver.ProcessMentions(ctx)

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
			slog.Error(fmt.Sprintf("http server error: %w", err))
		}
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)
	<-c // wait for interrupt
	slog.Info("interrupt received, shutting down")

	shutdownCtx, shutdownRelease := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownRelease()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error(fmt.Sprintf("http shutdown error: %w", err))
	}
}
