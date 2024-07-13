// Provides a service that listens for and processes incoming Webmentions.
// After some preliminary (synchronous) validation, Webmention requests are
// queued and then processed asynchronously.
//
// This application can be run (for example) as a daemon.
// A systemd service configuration is provided together with the source code.
//
// Configuration is read from a .env file (or just the env vars directly).
// An .env file must be present in either the process working directory
// `$PWD/.env`, or in `/etc/webmention/mentioner.env`.
// 
// Configurable values are:
//   - SHUTDOWN_TIMEOUT=Seconds: How long to wait for a clean shutdown after SIGINT or SIGTERM (default 20)
//   - ENDPOINT=URL Path: On which path to listen for Webmentions (default /api/webmention)
//   - LISTEN_ADDR=Domain with Port: Bind listener to this domain:port (default :8080)
//   - NOTIFY_BY_MAIL=yes or no: Whether or not to enable notifications by mail (default no)
//   - MAIL_HOST=Domain: Domain of the outgoing mail server (no default, required by NOTIFY_BY_MAIL)
//   - MAIL_PORT=Port: Port of the outgoing mail server (no default, required by NOTIFY_BY_MAIL)
//   - MAIL_USER=Username: User to authenticate to the outgoing mail server (no default, required by NOTIFY_BY_MAIL)
//   - MAIL_PASS=Password: Password to authenticate to the outgoing mail server (no default, required by NOTIFY_BY_MAIL)
//   - MAIL_FROM=E-Mail address: Address used in the FROM header (default same as MAIL_USER)
//   - MAIL_TO=E-Mail address: Address used in the TO header (default same as MAIL_FROM or MAIL_USER)
//
// Configuration is reloaded on SIGHUP.
package main

import (
	"strings"
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
	"strconv"

	"gopkg.in/gomail.v2"
	"github.com/joho/godotenv"
	webmention "github.com/cvanloo/gowebmention"
	"github.com/cvanloo/gowebmention/listener"
)

func init() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
}

const (
	ExitSuccess = 0
	ExitFailure = 1
	ExitConfigError = -1
)

const (
	defaultShutdownTimeout = 20 * time.Second
	defaultEndpoint = "/api/webmention"
	defaultListenAddr = ":8080"
	defaultNotifyByMail = false
)

var (
	shutdownTimeout = defaultShutdownTimeout
	endpoint = defaultEndpoint
	listenAddr = defaultListenAddr
	notifyByMail = defaultNotifyByMail
)

func loadConfig() {
	if err := godotenv.Load(); err != nil {
		_ = godotenv.Load("/etc/webmention/mentioner.env") // ignore error, use defaults
	}

	shutdownTimeout = defaultShutdownTimeout
	if timeoutStr := os.Getenv("SHUTDOWN_TIMEOUT"); timeoutStr != "" {
		if timeout, err := strconv.Atoi(timeoutStr); err == nil {
			shutdownTimeout = time.Duration(timeout) * time.Second
		}
	}

	endpoint = defaultEndpoint
	if endpointStr := os.Getenv("ENDPOINT"); endpointStr != "" {
		endpoint = endpointStr
	}

	listenAddr = defaultListenAddr
	if listenAddrStr := os.Getenv("LISTEN_ADDR"); listenAddrStr != "" {
		listenAddr = listenAddrStr
	}

	notifyByMail = defaultNotifyByMail
	if notifyByMailStr := os.Getenv("NOTIFY_BY_MAIL"); notifyByMailStr != "" {
		notifyByMail = wordToBool(notifyByMailStr)
	}
}

func main() {
	reload := make(chan os.Signal, 1)
	signal.Notify(reload, syscall.SIGHUP) // kill -HUP $(pidof mentionee)

	exit := make(chan os.Signal, 1)
	signal.Notify(exit, syscall.SIGINT, syscall.SIGTERM/*, syscall.SIGQUIT*/) // kill -TERM $(pidof mentionee)

appLoop:
	for {
		loadConfig()

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
			configureOrNil(notifyByMail, configureMailer()),
		)

		go receiver.ProcessMentions()

		mux := &http.ServeMux{}
		mux.HandleFunc(endpoint, receiver.WebmentionEndpoint)

		server := http.Server{
			Addr:    listenAddr,
			Handler: mux,
		}

		go func() {
			err := server.ListenAndServe()
			//err := server.ListenAndServeTLS()
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error(fmt.Sprintf("http server error: %s", err))
			}
		}()

		doShutdown := func() {
			shutdownCtx, shutdownRelease := context.WithTimeout(context.Background(), shutdownTimeout)
			server.SetKeepAlivesEnabled(false)
			defer shutdownRelease()
			if err := server.Shutdown(shutdownCtx); err != nil {
				slog.Error(fmt.Sprintf("http shutdown error: %s", err))
			}
			receiver.Shutdown(shutdownCtx)
		}

		select {
		case <-reload:
			slog.Info("sighup received, reloading configuration")
			doShutdown()
			continue appLoop
		case <-exit:
			slog.Info("interrupt received, shutting down")
			doShutdown()
			os.Exit(ExitSuccess)
			return
		}
	}
}

func wordToBool(word string) bool {
	meansYes := []string{"true", "yes", "y"}
	for _, yes := range meansYes {
		if strings.ToLower(word) == yes {
			return true
		}
	}
	return false
}

func configureOrNil(shouldConfigure bool, option webmention.ReceiverOption) webmention.ReceiverOption {
	if shouldConfigure {
		return option
	}
	return nil
}

func configureMailer() webmention.ReceiverOption {
	portStr := os.Getenv("MAIL_PORT")
	port, err := strconv.Atoi(portStr)
	if err != nil {
		slog.Error("invalid or missing mail port", "port", portStr)
		os.Exit(ExitConfigError)
		return nil
	}
	host := os.Getenv("MAIL_HOST")
	if host == "" {
		slog.Error("missing mail host")
		os.Exit(ExitConfigError)
		return nil
	}
	user := os.Getenv("MAIL_USER")
	if user == "" {
		slog.Error("missing mail user")
		os.Exit(ExitConfigError)
		return nil
	}
	pass := os.Getenv("MAIL_PASS")
	if pass == "" {
		slog.Error("missing mail pass")
		os.Exit(ExitConfigError)
		return nil
	}
	sendMailsFrom := os.Getenv("MAIL_FROM")
	if sendMailsFrom == "" {
		sendMailsFrom = user
	}
	sendMailsTo := os.Getenv("MAIL_TO")
	if sendMailsTo == "" {
		sendMailsTo = sendMailsFrom
	}
	dialer := gomail.NewDialer(host, port, user, pass)
	mailer := listener.NewMailer(dialer, sendMailsFrom, sendMailsTo)
	return webmention.WithNotifier(mailer)
}
