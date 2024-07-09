package listener

import (
	"fmt"
	"gopkg.in/gomail.v2"
	"log/slog"

	webmention "github.com/cvanloo/gowebmention"
)

// Configure a mailer.
// The SubjectLine and Body properties of the Mailer may be modified to
// generate custom email subjects and contents, when notifying about Webmentions.
func NewMailer(dialer *gomail.Dialer, sender, receiver string) Mailer {
	return Mailer{
		Sender:   sender,
		Receiver: receiver,
		Dialer:   dialer,
		SubjectLine: func(webmention.IncomingMention, webmention.Status) string {
			return "A post of yours has been mentioned"
		},
		Body: func(mention webmention.IncomingMention, status webmention.Status) string {
			return fmt.Sprintf("source: %s\ntarget: %s\nstatus: %s\n", mention.Source, mention.Target, status) // @todo: bake status into the mention
		},
	}
}

// Mailer is a webmention.Notifier that -- whenever a mention is received --
// sends an email notification from Sender to Receiver, with a subject line
// produced by SubjectLine and the email body produced by Body.
type Mailer struct {
	Sender, Receiver string
	Dialer           *gomail.Dialer
	SubjectLine      func(webmention.IncomingMention, webmention.Status) string
	Body             func(webmention.IncomingMention, webmention.Status) string
}

// Receive implements webmention.Notifier
func (m Mailer) Receive(mention webmention.IncomingMention, status webmention.Status) {
	msg := gomail.NewMessage()
	msg.SetHeader("From", m.Sender)
	msg.SetHeader("To", m.Receiver)
	msg.SetHeader("Subject", m.SubjectLine(mention, status))
	msg.SetBody("text/plain", m.Body(mention, status))
	err := m.Dialer.DialAndSend(msg)
	if err != nil {
		slog.Error(fmt.Sprintf("NotifyByMail: failed to send email: %s", err),
			"mention", mention,
			"status", status,
		)
	}
}
