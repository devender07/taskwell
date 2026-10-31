// Package mail queues plain-text email in outbound_mail and sends it over SMTP, using settings from system_config,
// from a worker, so no request waits on SMTP.
package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/user/internal/sysconfig"
)

type Mailer struct {
	db          *pgxpool.Pool
	cfg         *sysconfig.Store
	wake        chan struct{}
	timeout     time.Duration // one SMTP conversation, dial to QUIT
	retryBase   time.Duration // wait after the first failure; doubles each attempt, up to maxRetryWait
	maxAttempts int
}

const maxRetryWait = time.Hour

// New returns a Mailer whose sends take at most timeout and whose failures retry after retryBase, doubling, until
// maxAttempts sends have failed.
func New(db *pgxpool.Pool, cfg *sysconfig.Store, timeout, retryBase time.Duration, maxAttempts int) *Mailer {
	return &Mailer{db: db, cfg: cfg, wake: make(chan struct{}, 1), timeout: timeout, retryBase: retryBase, maxAttempts: maxAttempts}
}

// Configured reports whether an SMTP host is set.
func (m *Mailer) Configured(ctx context.Context) (bool, error) {
	host, err := m.cfg.Get(ctx, "smtp_host")
	return host != "", err
}

// Execer is a pool or a transaction.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Enqueue queues a plain-text message to one recipient through q (pass a transaction to queue it only if that
// commits) and wakes the worker.
func (m *Mailer) Enqueue(ctx context.Context, q Execer, to, subject, body string) error {
	if _, err := q.Exec(ctx, `insert into outbound_mail (recipient, subject, body) values ($1, $2, $3)`, to, subject, body); err != nil {
		return fmt.Errorf("queue mail: %w", err)
	}
	m.Wake()
	return nil
}

// Wake makes the worker look for due mail now.
func (m *Mailer) Wake() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// Run sends due mail until ctx ends: when woken, and every 10s for retries and mail other instances queued.
func (m *Mailer) Run(ctx context.Context) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		for ctx.Err() == nil {
			sent, err := m.sendNext(ctx)
			if err != nil && ctx.Err() == nil {
				slog.Error("mail queue failed", "err", err)
			}
			if !sent || err != nil {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-m.wake:
		}
	}
}

// retryWait is how long to wait after the attempts-th failed send.
func retryWait(base time.Duration, attempts int) time.Duration {
	if attempts > 20 {
		return maxRetryWait
	}
	return min(base<<(attempts-1), maxRetryWait)
}

type smtpConfig struct{ host, port, user, pass, from string }

// sendNext claims the oldest due message (committed before sending, so no transaction waits on SMTP), sends it and
// deletes it, or schedules a retry. It returns false when nothing is due or SMTP isn't configured.
func (m *Mailer) sendNext(ctx context.Context) (bool, error) {
	var c [5]string
	for i, k := range []string{"smtp_host", "smtp_port", "smtp_username", "smtp_password", "smtp_from"} {
		v, err := m.cfg.Get(ctx, k)
		if err != nil {
			return false, err
		}
		c[i] = v
	}
	s := smtpConfig{c[0], c[1], c[2], c[3], c[4]}
	if s.host == "" {
		return false, nil
	}
	var id, to, subject, body string
	var attempts int
	err := m.db.QueryRow(ctx,
		`update outbound_mail set attempts = attempts + 1, next_attempt_at = now() + make_interval(secs => $1)
		 where id = (select id from outbound_mail where failed_at is null and next_attempt_at <= now()
		             order by next_attempt_at limit 1 for update skip locked)
		 returning id::text, recipient, subject, body, attempts`, (m.timeout+30*time.Second).Seconds()).
		Scan(&id, &to, &subject, &body, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim mail: %w", err)
	}
	sendErr := m.send(ctx, s, to, subject, body)
	if ctx.Err() != nil {
		return false, nil // shutting down: the claim expires and the next run retries it
	}
	if sendErr == nil {
		_, err = m.db.Exec(ctx, `delete from outbound_mail where id = $1`, id)
	} else if attempts >= m.maxAttempts {
		slog.Error("mail gave up", "id", id, "attempts", attempts, "err", sendErr)
		_, err = m.db.Exec(ctx, `update outbound_mail set failed_at = now(), last_error = $2 where id = $1`, id, sendErr.Error())
	} else {
		wait := retryWait(m.retryBase, attempts)
		slog.Warn("mail send failed, will retry", "id", id, "attempts", attempts, "retry_in", wait, "err", sendErr)
		_, err = m.db.Exec(ctx, `update outbound_mail set next_attempt_at = now() + make_interval(secs => $2), last_error = $3
			where id = $1`, id, wait.Seconds(), sendErr.Error())
	}
	if err != nil {
		return true, fmt.Errorf("record mail %s: %w", id, err)
	}
	return true, nil
}

// send delivers one message within m.timeout, using STARTTLS when the server offers it. The subject is Q-encoded
// when needed, which also neutralizes CR/LF from user-supplied names.
func (m *Mailer) send(ctx context.Context, s smtpConfig, to, subject, body string) error {
	sender, err := mail.ParseAddress(s.from)
	if err != nil {
		return fmt.Errorf("parse smtp_from: %w", err)
	}
	dctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(dctx, "tcp", net.JoinHostPort(s.host, s.port))
	if err != nil {
		return fmt.Errorf("dial smtp: %w", err)
	}
	defer conn.Close()
	deadline, _ := dctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() }) // shutdown ends the conversation too
	defer stop()
	cl, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return fmt.Errorf("smtp greeting: %w", err)
	}
	defer cl.Close()
	if ok, _ := cl.Extension("STARTTLS"); ok {
		if err := cl.StartTLS(&tls.Config{ServerName: s.host}); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}
	if s.user != "" {
		if err := cl.Auth(smtp.PlainAuth("", s.user, s.pass, s.host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	msg := "From: " + s.from + "\r\nTo: " + to + "\r\nSubject: " + mime.QEncoding.Encode("UTF-8", subject) +
		"\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" +
		strings.ReplaceAll(body, "\n", "\r\n")
	if err := cl.Mail(sender.Address); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := cl.Rcpt(to); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}
	w, err := cl.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp end data: %w", err)
	}
	_ = cl.Quit() // the server has accepted the message: retrying on a failed QUIT would send it twice
	return nil
}
