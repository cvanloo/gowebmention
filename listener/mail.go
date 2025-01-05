package listener

import (
	"fmt"
	"gopkg.in/gomail.v2"
	"log/slog"
	"bytes"
	"net/smtp"
	"sync"
	"time"
	"strings"

	"github.com/emersion/go-msgauth/dkim"

	webmention "github.com/cvanloo/gowebmention"
)

type (
	Mailer struct {
		Sender Sender
	}
	Sender interface {
		Send([]webmention.Mention) error
	}
	ReportAggregator struct {
		m              sync.Mutex
		Todos          []webmention.Mention
		SendAfterTime  time.Duration
		lastSentTime   time.Time
		SendAfterCount int
		Sender         Sender
	}
	InternalMailer struct {
		SubjectLine      func([]webmention.Mention) string
		Body             func([]webmention.Mention) string
		FromAddr, ToAddr string
		From, To         string
	}
	InternalDKIMMailer struct {
		InternalMailer
		DkimSignOpts     *dkim.SignOptions
	}
	ExternalMailer struct {
		SubjectLine func([]webmention.Mention) string
		Body        func([]webmention.Mention) string
		From, To    string
		Dialer      *gomail.Dialer
	}
)

func DefaultSubjectLine(mentions []webmention.Mention) string {
	return fmt.Sprintf("You've received %d new mentions", len(mentions))
}

func DefaultBody(mentions []webmention.Mention) string {
	var builder strings.Builder
	for _, mention := range mentions {
		builder.WriteString(fmt.Sprintf("source: %s\ntarget: %s\nstatus: %s\n\n", mention.Source, mention.Target, mention.Status))
	}
	return builder.String()
}

func NewMailer(sender Sender) Mailer {
	return Mailer{Sender: sender}
}

func (m Mailer) Receive(mention webmention.Mention) {
	if err := m.Sender.Send([]webmention.Mention{mention}); err != nil {
		slog.Error(fmt.Sprintf("notifybymail: failed to send email: %s", err), "mention", mention)
	}
}

func (m *ReportAggregator) Start() {
	for range time.Tick(m.SendAfterTime) {
		if m.m.TryLock() {
			m.SendNow()
			m.m.Unlock()
		}
	}
}

func (m *ReportAggregator) Send(mentions []webmention.Mention) error {
	m.m.Lock()
	defer m.m.Unlock()
	m.Todos = append(m.Todos, mentions...)
	switch {
	case time.Now().Sub(m.lastSentTime) >= m.SendAfterTime:
		fallthrough
	case m.SendAfterCount > 0 && len(m.Todos) >= m.SendAfterCount:
		return m.SendNow()
	}
	return nil
}

func (m *ReportAggregator) SendNow() error {
	if len(m.Todos) <= 0 {
		return nil // not an error, just do nothing
	}
	if err := m.Sender.Send(m.Todos); err != nil {
		return err
	}
	m.Todos = nil
	m.lastSentTime = time.Now()
	return nil
}

func (m InternalMailer) Send(mentions []webmention.Mention) error {
	msg := gomail.NewMessage()
	msg.SetHeader("From", m.From)
	msg.SetHeader("To", m.To)
	msg.SetHeader("Subject", m.SubjectLine(mentions))
	msg.SetBody("text/plain", m.Body(mentions))
	var clearMessage bytes.Buffer
	if _, err := msg.WriteTo(&clearMessage); err != nil {
		return err
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
	if _, err := w.Write(clearMessage.Bytes()); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	if err := c.Quit(); err != nil {
		return err
	}
	return nil
}

func (m InternalDKIMMailer) Send(mentions []webmention.Mention) error {
	msg := gomail.NewMessage()
	msg.SetHeader("From", m.From)
	msg.SetHeader("To", m.To)
	msg.SetHeader("Subject", m.SubjectLine(mentions))
	msg.SetBody("text/plain", m.Body(mentions))
	var clearMessage, signedMessage bytes.Buffer
	if _, err := msg.WriteTo(&clearMessage); err != nil {
		return err
	}
	if err := dkim.Sign(&signedMessage, &clearMessage, m.DkimSignOpts); err != nil {
		return err
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
	if _, err := w.Write(signedMessage.Bytes()); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	if err := c.Quit(); err != nil {
		return err
	}
	return nil
}

func (m ExternalMailer) Send(mentions []webmention.Mention) error {
	msg := gomail.NewMessage()
	msg.SetHeader("From", m.From)
	msg.SetHeader("To", m.To)
	msg.SetHeader("Subject", m.SubjectLine(mentions))
	msg.SetBody("text/plain", m.Body(mentions))
	return m.Dialer.DialAndSend(msg)
}
