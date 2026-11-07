package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	stripeVersion  = "2026-09-30.endive"
	stripeBase     = "https://api.stripe.com"
	sigTolerance   = 5 * time.Minute
	maxStripeReply = 1 << 20
)

// stripeAPI is the few Stripe calls billing makes, form-encoded over net/http.
type stripeAPI struct {
	base   string // stripeBase; tests point it at httptest
	client *http.Client
}

// call sends form (as the query on a GET) to path with the secret key and decodes the reply into out. idem, when not
// "", is the Idempotency-Key.
func (s *stripeAPI) call(ctx context.Context, key, method, path string, form url.Values, idem string, out any) error {
	var body io.Reader
	if method == http.MethodGet && len(form) > 0 {
		path += "?" + form.Encode()
	} else if len(form) > 0 {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, s.base+path, body)
	if err != nil {
		return fmt.Errorf("stripe request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Stripe-Version", stripeVersion)
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	res, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("stripe %s: %w", method, err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, maxStripeReply))
	if err != nil {
		return fmt.Errorf("stripe %s %s: read: %w", method, req.URL.Path, err)
	}
	if res.StatusCode >= 300 {
		var e struct {
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(b, &e) // a reply that isn't Stripe's error shape still reports the status
		return fmt.Errorf("stripe %s %s: %d %s: %s", method, req.URL.Path, res.StatusCode, e.Error.Type, e.Error.Message)
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("stripe %s %s: decode: %w", method, req.URL.Path, err)
	}
	return nil
}

// subscription is the part of a Stripe subscription sync reads. The period end is on the item on current API versions.
type subscription struct {
	ID                string `json:"id"`
	Customer          string `json:"customer"`
	Status            string `json:"status"`
	Currency          string `json:"currency"`
	CancelAtPeriodEnd bool   `json:"cancel_at_period_end"`
	Items             struct {
		Data []struct {
			CurrentPeriodEnd int64 `json:"current_period_end"`
			Price            struct {
				LookupKey string `json:"lookup_key"`
			} `json:"price"`
		} `json:"data"`
	} `json:"items"`
}

type price struct {
	ID              string `json:"id"`
	LookupKey       string `json:"lookup_key"`
	Currency        string `json:"currency"`
	UnitAmount      int64  `json:"unit_amount"`
	CurrencyOptions map[string]struct {
		UnitAmount int64 `json:"unit_amount"`
	} `json:"currency_options"`
}

// event is a webhook event; Object is the object it's about.
type event struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Data struct {
		Object json.RawMessage `json:"object"`
	} `json:"data"`
}

// verifySignature reports whether header (Stripe-Signature: t=<unix>,v1=<hex>,...) signs body with secret: the
// HMAC-SHA256 of "<t>.<body>" matches any v1, and t is within sigTolerance of now.
func verifySignature(header string, body []byte, secret string, now time.Time) bool {
	var t string
	var sigs []string
	for part := range strings.SplitSeq(header, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch k {
		case "t":
			t = v
		case "v1":
			sigs = append(sigs, v)
		}
	}
	ts, err := strconv.ParseInt(t, 10, 64)
	if err != nil || secret == "" {
		return false
	}
	if d := now.Sub(time.Unix(ts, 0)); d > sigTolerance || d < -sigTolerance {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(t + "."))
	mac.Write(body)
	want := mac.Sum(nil)
	for _, s := range sigs {
		if got, err := hex.DecodeString(s); err == nil && hmac.Equal(got, want) {
			return true
		}
	}
	return false
}
