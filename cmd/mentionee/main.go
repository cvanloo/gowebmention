// Provides a service that listens for and processes incoming Webmentions.
// After some preliminary (synchronous) validation, Webmention requests are
// queued and then processed asynchronously.
//
// This application can be run (for example) as a daemon.
// A systemd service configuration is provided together with the source code.
//
// Configuration is read from a .env file (or just the OS env vars directly).
// An .env file must be present in either the process working directory
// `$PWD/.env`, or in `/etc/webmention/mentionee.env`.
//
// Configurable values are:
//   - SHUTDOWN_TIMEOUT=Seconds: How long to wait for a clean shutdown after SIGINT or SIGTERM (default 120)
//   - ENDPOINT=URL Path: On which path to listen for Webmentions (default /api/webmention)
//   - LISTEN_ADDR=Domain with Port: Bind listener to this domain:port (default :8080)
//   - ACCEPT_DOMAIN=Domain: Accept mentions if they point to this domain (e.g., the domain of your blog, required, no default)
//   - NOTIFY_BY_MAIL=external, internal or no: Whether or not to enable notifications by mail (default no)
//
// Options for external SMTP server:
//   - MAIL_HOST=Domain: Domain of the outgoing mail server (no default, required)
//   - MAIL_PORT=Port: Port of the outgoing mail server (no default, required)
//   - MAIL_USER=Username: User to authenticate to the outgoing mail server (no default, required)
//   - MAIL_PASS=Password: Password to authenticate to the outgoing mail server (no default, required)
//   - MAIL_FROM=E-Mail address: Address used in the FROM header (default same as MAIL_USER)
//   - MAIL_TO=E-Mail address: Address used in the TO header (default same as MAIL_FROM, or MAIL_USER if MAIL_FROM not set)
//
// Options for internal SMPT server:
//   - MAIL_FROM=E-Mail address: Send emails from this address (required)
//   - MAIL_TO=E-Mail address: Send emails to this email address (required)
//   - MAIL_FROM_ADDR=Domain: Domain from which to send mails (required)
//   - MAIL_TO_ADDR=Domain: Domain of the receiving mail server (required)
//   - MAIL_DKIM_PRIV=Path to private key: Path to private key used for dkim signing (default empty, don't sign)
//   - MAIL_DKIM_SELECTOR=Selector: DKIM selector (default is "default")
//   - MAIL_DKIM_HOST=Domain: Domain on which DKIM is configured
//
// For more information on how to setup the internal mail server, check the
// documentation on ConfigMailInternal.
//
// Configuration is reloaded on SIGHUP.
package main

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cvanloo/parsenv"
	"github.com/emersion/go-msgauth/dkim"
	"github.com/joho/godotenv"
	"gopkg.in/gomail.v2"

	webmention "github.com/cvanloo/gowebmention"
	"github.com/cvanloo/gowebmention/listener"
)

func init() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
}

var Config struct {
	ShutdownTimeout int    `cfg:"default=120"`
	EndpointUrl     string `cfg:"default=/api/webmention"`
	ListenAddr      string `cfg:"default=:8080"`
	AcceptDomain    string `cfg:"required"`
	NotifyByMail    string `cfg:"default=no"`
}

var ConfigMailExternal struct {
	MailHost string `cfg:"required"`
	MailPort int    `cfg:"required"`
	MailUser string `cfg:"required"`
	MailPass string `cfg:"required"`
	MailFrom string
	MailTo   string
}

// Mentionee can deliver emails directly to your inbox.
// For this, it needs to be authorized to send emails from the MAIL_FROM_ADDR
// domain. The following DNS entries are required:
//
//   - A   | mail.example.com   | 11.22.33.44
//   - MX  | example.com        | mail.example.com
//   - TXT | example.com        | v=spf1 mx -all
//   - TXT | _dmarc.example.com | v=DMARC1; p=quarantine;
//
// Replace example.com with your own domain, and 11.22.33.44 with the IP of the
// server on which mentionee runs.
//
// For DKIM refer to the documentation on ConfigMailDkim.
var ConfigMailInternal struct {
	MailFrom     string `cfg:"required"`
	MailTo       string `cfg:"required"`
	MailFromAddr string `cfg:"required"`
	MailToAddr   string `cfg:"required"`
	MailDkimPriv string
}

// In addition to the DNS entries explained in ConfigMailInternal, you'll have
// to setup another DNS entry for DKIM verification to work.
//
//   - TXT | default._domainkey.example.com | v=DKIM1; k=rsa; p=YOUR_PUBLIC_KEY_HERE
//
// "default" must be the same as in MAIL_DKIM_SELECTOR.
// You can create a pub/priv key pair using the commands:
//
//	openssl genrsa -out private 1024
//	openssl rsa -in private -pubout -out public
//	sed '1d;$d' public | tr -d '\n' > spublic; echo "" >> spublic
//
// MAIL_DKIM_PRIV must point to the 'private' file.
// As YOUR_PUBLIC_KEY_HERE in the above DNS entry, you must use the contents
// from the 'spublic' file.
//
// MAIL_DKIM_HOST is your domain, e.g., example.com.
//
// Since mentionee can only send, not receive emails, you might want to setup
// 'rua' and 'ruf' to point to a different email address:
//
//   - TXT | _dmarc.example.com | v=DMARC1; p=quarantine; rua=mailto:dmarc@whatever.else; ruf=mailto:dmarc-forensics@whatever.else;
//
// For this to work, you also need an entry on the whatever.else domain:
//
//   - TXT | example.com._report._dmarc | v=DMARC1
var ConfigMailDkim struct {
	MailDkimSelector string `cfg:"default=default"`
	MailDkimHost     string `cfg:"required"`
}

const (
	ExitSuccess     = 0
	ExitFailure     = 1
	ExitConfigError = -1
)

func loadConfig() (opts []webmention.ReceiverOption, listenAddr, endpoint string, shutdownTimeout time.Duration, agg *listener.ReportAggregator, err error) {
	if err := godotenv.Load(); err != nil {
		godotenv.Load("/etc/webmention/mentionee.env")
	}
	if err := parsenv.Load(&Config); err != nil {
		return opts, listenAddr, endpoint, shutdownTimeout, agg, err
	}
	listenAddr = Config.ListenAddr
	endpoint = Config.EndpointUrl
	shutdownTimeout = time.Duration(Config.ShutdownTimeout) * time.Second
	acceptDomain, err := url.Parse(Config.AcceptDomain)
	if err != nil {
		return opts, listenAddr, endpoint, shutdownTimeout, agg, err
	}
	opts = append(opts, webmention.WithAcceptsFunc(func(source, target *url.URL) bool {
		return target.Scheme == acceptDomain.Scheme && target.Host == acceptDomain.Host
	}))
	if Config.NotifyByMail == "external" {
		if err := parsenv.Load(&ConfigMailExternal); err != nil {
			return opts, listenAddr, endpoint, shutdownTimeout, agg, err
		}
		dialer := gomail.NewDialer(ConfigMailExternal.MailHost, ConfigMailExternal.MailPort, ConfigMailExternal.MailUser, ConfigMailExternal.MailPass)
		from := ConfigMailExternal.MailUser
		if ConfigMailExternal.MailFrom != "" {
			from = ConfigMailExternal.MailFrom
		}
		to := from
		if ConfigMailExternal.MailTo != "" {
			to = ConfigMailExternal.MailTo
		}
		mailer := listener.ExternalMailer{
			SubjectLine: listener.DefaultSubjectLine,
			Body:        listener.DefaultBody,
			From:        from,
			To:          to,
			Dialer:      dialer,
		}
		aggregator := &listener.ReportAggregator{
			SendAfterTime:  12 * time.Hour,
			SendAfterCount: -1,
			Sender:         mailer,
		}
		opts = append(opts, webmention.WithNotifier(listener.Mailer{aggregator}))
		agg = aggregator
	} else if Config.NotifyByMail == "internal" {
		if err := parsenv.Load(&ConfigMailInternal); err != nil {
			return opts, listenAddr, endpoint, shutdownTimeout, agg, err
		}
		if ConfigMailInternal.MailDkimPriv != "" {
			if err := parsenv.Load(&ConfigMailDkim); err != nil {
				return opts, listenAddr, endpoint, shutdownTimeout, agg, err
			}
			pkbs, err := os.ReadFile(ConfigMailInternal.MailDkimPriv)
			if err != nil {
				return opts, listenAddr, endpoint, shutdownTimeout, agg, err
			}
			block, _ := pem.Decode(pkbs)
			if block == nil {
				return opts, listenAddr, endpoint, shutdownTimeout, agg, errors.New("failed to decode PEM block containing private key")
			}
			key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
			if err != nil {
				return opts, listenAddr, endpoint, shutdownTimeout, agg, err
			}
			pk, ok := key.(*rsa.PrivateKey)
			if !ok {
				return opts, listenAddr, endpoint, shutdownTimeout, agg, fmt.Errorf("not an RSA private key: %T", key)
			}
			mailer := listener.InternalDKIMMailer{
				InternalMailer: listener.InternalMailer{
					SubjectLine: listener.DefaultSubjectLine,
					Body:        listener.DefaultBody,
					FromAddr:    ConfigMailInternal.MailFromAddr,
					ToAddr:      ConfigMailInternal.MailToAddr,
					From:        ConfigMailInternal.MailFrom,
					To:          ConfigMailInternal.MailTo,
				},
				DkimSignOpts: &dkim.SignOptions{
					Domain:   ConfigMailDkim.MailDkimHost,
					Selector: ConfigMailDkim.MailDkimSelector,
					Signer:   pk,
				},
			}
			aggregator := &listener.ReportAggregator{
				SendAfterTime:  12 * time.Hour,
				SendAfterCount: -1,
				Sender:         mailer,
			}
			opts = append(opts, webmention.WithNotifier(listener.Mailer{aggregator}))
			agg = aggregator
		} else {
			mailer := listener.InternalMailer{
				SubjectLine: listener.DefaultSubjectLine,
				Body:        listener.DefaultBody,
				FromAddr:    ConfigMailInternal.MailFromAddr,
				ToAddr:      ConfigMailInternal.MailToAddr,
				From:        ConfigMailInternal.MailFrom,
				To:          ConfigMailInternal.MailTo,
			}
			aggregator := &listener.ReportAggregator{
				SendAfterTime:  12 * time.Hour,
				SendAfterCount: -1,
				Sender:         mailer,
			}
			opts = append(opts, webmention.WithNotifier(listener.Mailer{aggregator}))
			agg = aggregator
		}
	}
	return opts, listenAddr, endpoint, shutdownTimeout, agg, nil
}

type OptionsCollection []webmention.ReceiverOption

func (c OptionsCollection) Configuration(r *webmention.Receiver) {
	for _, f := range c {
		f(r)
	}
}

func main() {
	reload := make(chan os.Signal, 1)
	signal.Notify(reload, syscall.SIGHUP) // kill -HUP $(pidof mentionee)

	exit := make(chan os.Signal, 1)
	signal.Notify(exit, syscall.SIGINT, syscall.SIGTERM) // kill -TERM $(pidof mentionee)

appLoop:
	for {
		options, listenAddr, endpoint, shutdownTimeout, aggregator, err := loadConfig()
		if err != nil {
			slog.Error("erroneous configuration, *** all services stopped ***: ", "configError", err)
			slog.Error("...waiting for SIGHUP (reload config) or SIGTERM/INT (terminate)")
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
			webmention.WithNotifier(webmention.NotifierFunc(func(mention webmention.Mention) {
				slog.Info("received webmention",
					"source", mention.Source.String(),
					"target", mention.Target.String(),
					"status", mention.Status,
				)
			})),
			OptionsCollection(options).Configuration,
		)

		if aggregator != nil {
			go aggregator.Start()
		}
		go receiver.ProcessMentions()

		mux := &http.ServeMux{}
		mux.Handle(endpoint, receiver)

		server := http.Server{
			Addr:    listenAddr,
			Handler: mux,
		}

		go func() {
			err := server.ListenAndServe()
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
			if aggregator != nil {
				aggregator.SendNow()
			}
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
