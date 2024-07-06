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

const shutdownTimeout = 20 * time.Second

func main() {
	receiver := webmention.NewReceiver()

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
