package listener

import (
	"fmt"
	"gopkg.in/gomail.v2"
	"log/slog"
	"bytes"
	"net/smtp"

	"github.com/emersion/go-msgauth/dkim"

	webmention "github.com/cvanloo/gowebmention"
)

// Configure a mailer.
// The SubjectLine and Body properties of the Mailer may be modified to
// generate custom email subjects and contents, when notifying about Webmentions.
func NewMailerExternal(dialer *gomail.Dialer, sender, receiver string) MailerExternalSmtp {
	return MailerExternalSmtp{
		Sender:   sender,
		Receiver: receiver,
		Dialer:   dialer,
		SubjectLine: func(webmention.Mention) string {
			return "A post of yours has been mentioned"
		},
		Body: func(mention webmention.Mention) string {
			return fmt.Sprintf("source: %s\ntarget: %s\nstatus: %s\n", mention.Source, mention.Target, mention.Status)
		},
	}
}

func NewMailerInternal(receiverAddr string, sender, receiver string, useDkim bool, dkimOptions dkim.SignOptions) MailerInternalSmtp {
	return MailerInternalSmtp{
		Sender:   sender,
		Receiver: receiver,
		SubjectLine: func(webmention.Mention) string {
			return "A post of yours has been mentioned"
		},
		Body: func(mention webmention.Mention) string {
			return fmt.Sprintf("source: %s\ntarget: %s\nstatus: %s\n", mention.Source, mention.Target, mention.Status)
		},
		UseDkim: useDkim,
		DkimSignOpts: dkimOptions,
		Addr: receiverAddr,
	}
}

// Mailer is a webmention.Notifier that -- whenever a mention is received --
// sends an email notification from Sender to Receiver, with a subject line
// produced by SubjectLine and the email body produced by Body.
type (
	MailerExternalSmtp struct {
		Sender, Receiver string
		Dialer           *gomail.Dialer
		SubjectLine      func(webmention.Mention) string
		Body             func(webmention.Mention) string
	}
	MailerInternalSmtp struct {
		Sender, Receiver string
		SubjectLine      func(webmention.Mention) string
		Body             func(webmention.Mention) string
		UseDkim          bool
		DkimSignOpts     dkim.SignOptions
		Addr             string
	}
)

// Receive implements webmention.Notifier
func (m MailerExternalSmtp) Receive(mention webmention.Mention) {
	msg := gomail.NewMessage()
	msg.SetHeader("From", m.Sender)
	msg.SetHeader("To", m.Receiver)
	msg.SetHeader("Subject", m.SubjectLine(mention))
	msg.SetBody("text/plain", m.Body(mention))
	err := m.Dialer.DialAndSend(msg)
	if err != nil {
		slog.Error(fmt.Sprintf("NotifyByMail: failed to send email: %s", err), "mention", mention)
	}
}

// Receive implements webmention.Notifier
func (m MailerInternalSmtp) Receive(mention webmention.Mention) {
	msg := gomail.NewMessage()
	msg.SetHeader("From", m.Sender)
	msg.SetHeader("To", m.Receiver)
	msg.SetHeader("Subject", m.SubjectLine(mention))
	msg.SetBody("text/plain", m.Body(mention))
	var message bytes.Buffer
	_, err := msg.WriteTo(&message)
	if err != nil {
		slog.Error(fmt.Sprintf("NotifyByMail: failed to write mail contents: %s", err), "mention", mention)
		return
	}
	if m.UseDkim {
		var signedMessage bytes.Buffer
		if err := dkim.Sign(&signedMessage, &message, &m.DkimSignOpts); err != nil {
			slog.Error(fmt.Sprintf("NotifyByMail: failed to sign mail: %s", err), "mention", mention)
			return
		}
		if err := smtp.SendMail(m.Addr, nil, m.Sender, []string{m.Receiver}, signedMessage.Bytes()); err != nil {
			slog.Error(fmt.Sprintf("NotifyByMail: failed to send mail: %s", err), "mention", mention)
			return
		}
	} else {
		if err := smtp.SendMail(m.Addr, nil, m.Sender, []string{m.Receiver}, message.Bytes()); err != nil {
			slog.Error(fmt.Sprintf("NotifyByMail: failed to send mail: %s", err), "mention", mention)
			return
		}
	}
	slog.Error("email sent", "mention", mention)
}
