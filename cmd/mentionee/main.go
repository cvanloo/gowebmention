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
//   - ACCEPT_DOMAIN=Domain: Accept mentions if they point to this domain (e.g., the domain of your blog, required, no default)
//   - NOTIFY_BY_MAIL=external, internal or no: Whether or not to enable notifications by mail (default no)
//                    extarnl: use an external smtp server
//                    internal: use builtin smtp
// Required options for external smtp server:
//   - MAIL_HOST=Domain: Domain of the outgoing mail server (no default, required by NOTIFY_BY_MAIL)
//   - MAIL_PORT=Port: Port of the outgoing mail server (no default, required by NOTIFY_BY_MAIL)
//   - MAIL_USER=Username: User to authenticate to the outgoing mail server (no default, required by NOTIFY_BY_MAIL)
//   - MAIL_PASS=Password: Password to authenticate to the outgoing mail server (no default, required by NOTIFY_BY_MAIL)
//   - MAIL_FROM=E-Mail address: Address used in the FROM header (default same as MAIL_USER)
//   - MAIL_TO=E-Mail address: Address used in the TO header (default same as MAIL_FROM or MAIL_USER)
// Required options for internal smpt server:
//   - MAIL_FROM=Send emails from this email address
//   - MAIL_TO=Send emails to this email address
//   - MAIL_FROM_ADDR=Domain from which to send mails
//   - MAIL_TO_ADDR=Domain of the receiving mail server
//   - MAIL_DKIM_PRIV=Path to private key: Path to private key used for dkim signing (default don't sign)
//   - MAIL_DKIM_SELECTOR=Selector: DKIM selector (default is "default")
//   - MAIL_DKIM_HOST=Domain on which the DKIM is configured
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
	"crypto/x509"
	"crypto/rsa"
	"encoding/pem"

	"gopkg.in/gomail.v2"
	"github.com/joho/godotenv"
	"github.com/emersion/go-msgauth/dkim"
	"github.com/cvanloo/parsenv"

	webmention "github.com/cvanloo/gowebmention"
	"github.com/cvanloo/gowebmention/listener"
)

func init() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
}

var Config struct {
	ShutdownTimeout int    `cfg:"default=20"`
	EndpointUrl     string `cfg:"default=/api/webmention"`
	ListenAddr      string `cfg:"default=:8080"`
	AcceptDomain    string `cfg:"required"`
	NotifyByEmail   string `cfg:"default=no"`
}

var ConfigMailExternal struct {
	MailHost string
	MailPort int
	MailUser string
	MailPass string
	MailFrom string
	MailTo   string
}

var ConfigMailInternal struct {
	MailFrom         string `cfg:"required"`
	MailTo           string `cfg:"required"`
	MailFromAddr     string `cfg:"required"`
	MailToAddr       string `cfg:"required"`
	MailDkimPriv     string
}

var ConfigMailDkim struct {
	MailDkimSelector string `cfg:"default=default"`
	MailDkimHost     string `cfg:"required"`
}

const (
	ExitSuccess = 0
	ExitFailure = 1
	ExitConfigError = -1
)

func loadConfig() err {
	if err := parsenv.Load(&Config); err != nil {
		return err
	}
	if Config.NotifyByEmail == "external" {
		if err := parsenv.Load(&ConfigMailExternal); err != nil {
			return err
		}
	} else if Config.NotifyByEmail == "internal" {
		if err := parsenv.Load(&ConfigMailInternal); err != nil {
			return err
		}
		if ConfigMailInternal.MailDkimPriv != "" {
			if err := parsenv.Load(&ConfigMailDkim); err != nil {
				return err
			}
		}
	}
	return nil
}

func main() {
	reload := make(chan os.Signal, 1)
	signal.Notify(reload, syscall.SIGHUP) // kill -HUP $(pidof mentionee)

	exit := make(chan os.Signal, 1)
	signal.Notify(exit, syscall.SIGINT, syscall.SIGTERM/*, syscall.SIGQUIT*/) // kill -TERM $(pidof mentionee)

appLoop:
	for {
		if err := loadConfig(); err != nil {
			log.Printf("erroneous configuration, *** all services stopped ***: ", err)
			select {
			case <-reload:
				slog.Info("sighup received, reloading configuration")
				continue appLoop
			case <-exit:
				slog.Info("interrupt received, shutting down")
				os.Exit(ExitConfigError)
				return
			}
		}

		receiver := webmention.NewReceiver(
			webmention.WithAcceptsFunc(func(source, target *url.URL) bool {
				// @todo: as default value use same as listen addr
				return target.Scheme == acceptForDomain.Scheme && target.Host == acceptForDomain.Host
			}),
			webmention.WithNotifier(webmention.NotifierFunc(func(mention webmention.Mention) {
				slog.Info("received webmention",
					"source", mention.Source.String(),
					"target", mention.Target.String(),
					"status", mention.Status,
				)
			})),
			configureOrNil(Config.NotifyByEmail != "no", configureMailer),
		)

		go receiver.ProcessMentions()

		mux := &http.ServeMux{}
		mux.Handle(endpoint, receiver)

		server := http.Server{
			Addr:    listenAddr,
			Handler: mux,
		}

		go func() {
			err := server.ListenAndServe()
			//err := server.ListenAndServeTLS()
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error(fmt.Sprintf("http server error: %s", err))
				os.Exit(ExitFailure)
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

func configureOrNil(shouldConfigure bool, option func() webmention.ReceiverOption) webmention.ReceiverOption {
	if shouldConfigure {
		return option()
	}
	return nil
}

// @todo: this must happen in loadConfig()
func configureMailer() webmention.ReceiverOption {
	switch Config.NotifyByEmail {
	case "external":
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
		sendMailsFrom := os.Getenv("MAIL_FROM")
		if sendMailsFrom == "" {
			sendMailsFrom = user
		}
		sendMailsTo := os.Getenv("MAIL_TO")
		if sendMailsTo == "" {
			sendMailsTo = sendMailsFrom
		}
		portStr := os.Getenv("MAIL_PORT")
		port, err := strconv.Atoi(portStr)
		if err != nil {
			slog.Error("invalid or missing mail port", "port", portStr)
			os.Exit(ExitConfigError)
			return nil
		}
		pass := os.Getenv("MAIL_PASS")
		if pass == "" {
			slog.Error("missing mail pass")
			os.Exit(ExitConfigError)
			return nil
		}
		dialer := gomail.NewDialer(host, port, user, pass)
		mailer := listener.ExternalMailer{
			SubjectLine: listener.DefaultSubjectLine,
			Body: listener.DefaultBody,
			From: sendMailsFrom,
			To: sendMailsTo,
			Dialer: dialer,
		}
		aggregator := &listener.ReportAggregator{
			SendAfterTime: 12*time.Hour,
			SendAfterCount: 24,
			Sender: mailer,
		}
		go aggregator.Start()
		slog.Info("enabling email notifications (external smtp)")
		return webmention.WithNotifier(listener.Mailer{aggregator})
	case "internal":
		toAddr := os.Getenv("MAIL_TO_ADDR")
		if toAddr == "" {
			slog.Error("missing MAIL_TO_ADDR")
			os.Exit(ExitConfigError)
			return nil
		}
		to := os.Getenv("MAIL_TO")
		if to == "" {
			slog.Error("missing MAIL_TO")
			os.Exit(ExitConfigError)
			return nil
		}
		fromAddr := os.Getenv("MAIL_FROM_ADDR")
		if fromAddr == "" {
			slog.Error("missing MAIL_FROM_ADDR")
			os.Exit(ExitConfigError)
			return nil
		}
		from := os.Getenv("MAIL_FROM")
		if from == "" {
			slog.Error("missing MAIL_FROM")
			os.Exit(ExitConfigError)
			return nil
		}
		host := os.Getenv("MAIL_DKIM_HOST")
		if host == "" {
			slog.Error("missing MAIL_DKIM_HOST")
			os.Exit(ExitConfigError)
			return nil
		}
		internalMailer := listener.InternalMailer{
			SubjectLine: listener.DefaultSubjectLine,
			Body: listener.DefaultBody,
			FromAddr: fromAddr,
			ToAddr: toAddr,
			From: from,
			To: to,
		}
		var sender listener.Sender
		sender = internalMailer
		dkimPrivPath := os.Getenv("MAIL_DKIM_PRIV")
		if dkimPrivPath != "" {
			pkbs, err := os.ReadFile(dkimPrivPath)
			if err != nil {
				slog.Error(err.Error())
				os.Exit(ExitConfigError)
			}
			block, _ := pem.Decode(pkbs)
			if block == nil {
				slog.Error("failed to decode PEM block containing private key")
				os.Exit(ExitConfigError)
				return nil
			}
			key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
			if err != nil {
				slog.Error(err.Error())
				os.Exit(ExitConfigError)
				return nil
			}
			pk, ok := key.(*rsa.PrivateKey)
			if !ok {
				slog.Error("not an rsa private key")
				os.Exit(ExitConfigError)
				return nil
			}
			selector := os.Getenv("MAIL_DKIM_SELECTOR")
			if selector == "" {
				selector = "default"
			}
			dkimSignOpts := &dkim.SignOptions{
				Domain: host,
				Selector: selector,
				Signer: pk,
			}
			sender = listener.InternalDKIMMailer{
				InternalMailer: internalMailer,
				DkimSignOpts: dkimSignOpts,
			}
		}
		aggregator := &listener.ReportAggregator{
			SendAfterTime: 12*time.Hour,
			SendAfterCount: 24,
			Sender: sender,
		}
		go aggregator.Start()
		slog.Info("enabling email notifications (internal smtp)")
		return webmention.WithNotifier(listener.Mailer{aggregator})
	default:
		slog.Error("invalid option for NOTIFY_BY_MAIL", "value", notifyByMail)
		os.Exit(ExitConfigError)
	}
	return nil
}
