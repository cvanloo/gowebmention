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

func NewMailerInternal(fromAddr, from, toAddr, to string, useDkim bool, dkimOpts *dkim.SignOptions) MailerInternalSmtp {
	return MailerInternalSmtp{
		FromAddr: fromAddr,
		From: from,
		ToAddr: toAddr,
		To: to,
		SubjectLine: func(webmention.Mention) string {
			return "A post of yours has been mentioned"
		},
		Body: func(mention webmention.Mention) string {
			return fmt.Sprintf("source: %s\ntarget: %s\nstatus: %s\n", mention.Source, mention.Target, mention.Status)
		},
		UseDkim: useDkim,
		DkimSignOpts: dkimOpts,
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
		FromAddr, ToAddr string
		From, To string
		SubjectLine      func(webmention.Mention) string
		Body             func(webmention.Mention) string
		UseDkim          bool
		DkimSignOpts     *dkim.SignOptions
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
	if err := m.receive(mention); err != nil {
		slog.Error("MailerInternalSmtp receive failed to send email", "error", err, "mention", mention)
	} else {
		slog.Info("MailerInternalSmtp email sent", "mention", mention)
	}
}

// Receive implements webmention.Notifier
func (m MailerInternalSmtp) receive(mention webmention.Mention) error {
	msg := gomail.NewMessage()
	msg.SetHeader("From", m.From)
	msg.SetHeader("To", m.To)
	msg.SetHeader("Subject", m.SubjectLine(mention))
	msg.SetBody("text/plain", m.Body(mention))
	var clearMessage, signedMessage bytes.Buffer
	if _, err := msg.WriteTo(&clearMessage); err != nil {
		return err
	}
	if m.UseDkim {
		if err := dkim.Sign(&signedMessage, &clearMessage, m.DkimSignOpts); err != nil {
			return err
		}
	}
	c, err := smtp.Dial(m.ToAddr)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.Hello(m.FromAddr); err != nil {
		return err
	}
	if err := c.Mail(m.From); err != nil {
		return err
	}
	if err := c.Rcpt(m.To); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if m.UseDkim {
		if _, err := w.Write(signedMessage.Bytes()); err != nil {
			return err
		}
	} else {
		if _, err := w.Write(clearMessage.Bytes()); err != nil {
			return err
		}
	}
	if err := w.Close(); err != nil {
		return err
	}
	if err := c.Quit(); err != nil {
		return err
	}
	return nil
}
