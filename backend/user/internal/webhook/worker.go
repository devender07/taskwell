package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/devender07/tether/backend/user/internal/netguard"
	"github.com/devender07/tether/backend/user/internal/secret"
)

// retryWaits is how long to wait after each failed attempt; a delivery fails for good once they run out.
var retryWaits = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour, 12 * time.Hour}

const sendTimeout = 10 * time.Second

// newClient posts with a 10s timeout, no redirects and no proxy, dialing only public addresses unless allowPrivate.
func newClient(allowPrivate bool) *http.Client {
	return &http.Client{
		Timeout:       sendTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{
			Proxy:               nil,
			DialContext:         netguard.DialContext(netguard.Lookup, allowPrivate),
			TLSHandshakeTimeout: sendTimeout, IdleConnTimeout: 30 * time.Second,
		},
	}
}

// Wake makes the worker look for due deliveries now.
func (h *Handler) Wake() {
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

// Run sends due deliveries until ctx ends: when another instance's trigger notifies webhooks, when woken, and every
// 10s for retries.
// ponytail: one send at a time per instance, so a slow receiver delays the rest; send concurrently if queues grow.
func (h *Handler) Run(ctx context.Context) {
	var wg sync.WaitGroup
	defer wg.Wait()
	wg.Go(func() { h.listen(ctx) })
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		for ctx.Err() == nil {
			sent, err := h.sendNext(ctx)
			if err != nil && ctx.Err() == nil {
				slog.Error("webhook queue failed", "err", err)
			}
			if !sent || err != nil {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-h.wake:
		}
	}
}

// listen wakes the worker on each notification on webhooks until ctx ends, reconnecting with backoff.
func (h *Handler) listen(ctx context.Context) {
	wait := time.Second
	for {
		err := h.listenOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		slog.Warn("webhook listener down", "err", err, "retry_in", wait)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, 30*time.Second)
	}
}

func (h *Handler) listenOnce(ctx context.Context) error {
	pc, err := h.model.db.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire: %w", err)
	}
	conn := pc.Hijack() // a listening connection never goes back to the pool
	defer conn.Close(context.WithoutCancel(ctx))
	if _, err := conn.Exec(ctx, "listen webhooks"); err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	h.Wake() // deliveries queued while nothing listened
	for {
		if _, err := conn.WaitForNotification(ctx); err != nil {
			return fmt.Errorf("wait: %w", err)
		}
		h.Wake()
	}
}

// sendNext claims a due delivery, posts it and records the outcome; false when nothing is due.
func (h *Handler) sendNext(ctx context.Context) (bool, error) {
	j, err := h.model.claim(ctx)
	if err != nil || j == nil {
		return false, err
	}
	var code int
	var sendErr string
	if key, err := secret.Open(h.opts.Key, sealAD(j.wsID, j.webhookID), j.sealed); err != nil {
		slog.Error("webhook secret unreadable", "webhook", j.webhookID, "err", err)
		sendErr = "Tether couldn't read this webhook's secret. Roll the secret."
	} else {
		code, sendErr = h.post(ctx, j, key, time.Now())
	}
	if ctx.Err() != nil {
		return false, nil // shutting down: the claim expires and the next run retries it
	}
	codePtr, errPtr := optional(code), optional(clean(sendErr))
	status, retry := "delivered", time.Duration(0)
	switch {
	case sendErr == "":
	case j.attempts > len(retryWaits):
		status = "failed"
	default:
		status, retry = "pending", retryWaits[j.attempts-1]
	}
	if err := h.model.record(ctx, j, status, codePtr, errPtr, retry); err != nil {
		return true, fmt.Errorf("record delivery %s: %w", j.id, err)
	}
	return true, nil
}

// post sends the delivery signed with key at now, returning the response code (0 without a response) and, unless it
// was 2xx, what went wrong.
func (h *Handler) post(ctx context.Context, j *job, key string, now time.Time) (int, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, j.url, bytes.NewReader(j.payload))
	if err != nil {
		return 0, err.Error()
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Tether-Webhooks/1")
	req.Header.Set("X-Tether-Event", j.event)
	req.Header.Set("X-Tether-Delivery", j.id)
	req.Header.Set("X-Tether-Signature", Sign(key, now, j.payload))
	resp, err := h.client.Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp.StatusCode, ""
	}
	if msg := strings.TrimSpace(string(body)); msg != "" {
		return resp.StatusCode, msg
	}
	return resp.StatusCode, resp.Status
}

// Sign is the X-Tether-Signature header: t=<unix seconds>,v1=<hex HMAC-SHA256 of "<t>.<body>" keyed with the secret>.
func Sign(key string, now time.Time, body []byte) string {
	t := strconv.FormatInt(now.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(t + "."))
	mac.Write(body)
	return "t=" + t + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

// clean makes a response or error storable: valid UTF-8 without NULs, at most 500 characters.
func clean(s string) string {
	s = strings.ReplaceAll(strings.ToValidUTF8(s, "�"), "\x00", "")
	if r := []rune(s); len(r) > 500 {
		s = string(r[:500])
	}
	return s
}

func optional[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}
	return &v
}
