// Package mail sends plain-text email over SMTP using settings from system_config.
package mail

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"

	"github.com/devender07/tether/backend/user/internal/sysconfig"
)

// ErrNotConfigured means smtp_host is empty.
var ErrNotConfigured = errors.New("smtp not configured")

type Mailer struct{ cfg *sysconfig.Store }

func New(cfg *sysconfig.Store) *Mailer { return &Mailer{cfg: cfg} }

// Configured reports whether an SMTP host is set.
func (m *Mailer) Configured(ctx context.Context) (bool, error) {
	host, err := m.cfg.Get(ctx, "smtp_host")
	return host != "", err
}

// Send delivers a plain-text message to one recipient. The subject is Q-encoded when needed, which also
// neutralizes CR/LF from user-supplied names.
// ponytail: net/smtp ignores ctx and dials synchronously; move to a queue/worker if SMTP latency hurts requests.
func (m *Mailer) Send(ctx context.Context, to, subject, body string) error {
	var c [5]string
	for i, k := range []string{"smtp_host", "smtp_port", "smtp_username", "smtp_password", "smtp_from"} {
		v, err := m.cfg.Get(ctx, k)
		if err != nil {
			return err
		}
		c[i] = v
	}
	host, port, user, pass, from := c[0], c[1], c[2], c[3], c[4]
	if host == "" {
		return ErrNotConfigured
	}
	sender, err := mail.ParseAddress(from)
	if err != nil {
		return fmt.Errorf("parse smtp_from: %w", err)
	}
	var auth smtp.Auth
	if user != "" {
		auth = smtp.PlainAuth("", user, pass, host)
	}
	msg := "From: " + from + "\r\nTo: " + to + "\r\nSubject: " + mime.QEncoding.Encode("UTF-8", subject) +
		"\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" +
		strings.ReplaceAll(body, "\n", "\r\n")
	if err := smtp.SendMail(net.JoinHostPort(host, port), auth, sender.Address, []string{to}, []byte(msg)); err != nil {
		return fmt.Errorf("send mail: %w", err)
	}
	return nil
}
