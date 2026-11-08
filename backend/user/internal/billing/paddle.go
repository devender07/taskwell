package billing

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	sandboxBase    = "https://sandbox-api.paddle.com"
	productionBase = "https://api.paddle.com"
	sigTolerance   = 5 * time.Minute
	maxPaddleReply = 1 << 20
)

// paddleAPI is the few Paddle Billing calls billing makes, JSON over net/http.
type paddleAPI struct {
	base   string // "" picks the base by paddle_environment; tests point it at httptest
	client *http.Client
}

// call sends in (when not nil) as JSON to path in the environment env ("production" or else sandbox) with the API
// key, and decodes the reply's data into out.
func (p *paddleAPI) call(ctx context.Context, key, env, method, path string, in, out any) error {
	base := p.base
	if base == "" {
		base = sandboxBase
		if env == "production" {
			base = productionBase
		}
	}
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("paddle %s %s: encode: %w", method, path, err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return fmt.Errorf("paddle request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("paddle %s: %w", method, err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, maxPaddleReply))
	if err != nil {
		return fmt.Errorf("paddle %s %s: read: %w", method, path, err)
	}
	if res.StatusCode >= 300 {
		var e struct {
			Error struct {
				Code   string `json:"code"`
				Detail string `json:"detail"`
			} `json:"error"`
		}
		_ = json.Unmarshal(b, &e) // a reply that isn't Paddle's error shape still reports the status
		return fmt.Errorf("paddle %s %s: %d %s: %s", method, path, res.StatusCode, e.Error.Code, e.Error.Detail)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(b, &struct {
		Data any `json:"data"`
	}{out}); err != nil {
		return fmt.Errorf("paddle %s %s: decode: %w", method, path, err)
	}
	return nil
}

// subscription is the part of a Paddle subscription sync reads.
type subscription struct {
	ID              string     `json:"id"`
	Status          string     `json:"status"`
	CustomerID      string     `json:"customer_id"`
	TransactionID   string     `json:"transaction_id"` // on subscription.created: the checkout's transaction
	CurrencyCode    string     `json:"currency_code"`
	NextBilledAt    *time.Time `json:"next_billed_at"`
	ScheduledChange *struct {
		Action string `json:"action"`
	} `json:"scheduled_change"`
	CustomData struct {
		UserID string `json:"user_id"`
	} `json:"custom_data"`
	Items []struct {
		Price struct {
			ID string `json:"id"`
		} `json:"price"`
	} `json:"items"`
}

type money struct {
	Amount       string `json:"amount"` // smallest unit, as a string
	CurrencyCode string `json:"currency_code"`
}

type price struct {
	ID                 string `json:"id"`
	UnitPrice          money  `json:"unit_price"`
	UnitPriceOverrides []struct {
		CountryCodes []string `json:"country_codes"`
		UnitPrice    money    `json:"unit_price"`
	} `json:"unit_price_overrides"`
}

// amounts is the price's base USD amount and its India override's INR amount; ok is false when either is missing.
func (p price) amounts() (usd, inr int64, ok bool) {
	parse := func(m money, currency string) (int64, bool) {
		n, err := strconv.ParseInt(m.Amount, 10, 64)
		return n, err == nil && m.CurrencyCode == currency
	}
	usd, okUSD := parse(p.UnitPrice, "USD")
	for _, o := range p.UnitPriceOverrides {
		for _, c := range o.CountryCodes {
			if c == "IN" {
				var okINR bool
				inr, okINR = parse(o.UnitPrice, "INR")
				return usd, inr, okUSD && okINR
			}
		}
	}
	return usd, 0, false
}

// event is a webhook notification; Data is the entity it's about.
type event struct {
	ID   string          `json:"event_id"`
	Type string          `json:"event_type"`
	Data json.RawMessage `json:"data"`
}

// verifySignature reports whether header (Paddle-Signature: ts=<unix>;h1=<hex>;...) signs body with secret: the
// HMAC-SHA256 of "<ts>:<body>" matches any h1, and ts is within sigTolerance of now.
func verifySignature(header string, body []byte, secret string, now time.Time) bool {
	var t string
	var sigs []string
	for part := range strings.SplitSeq(header, ";") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch k {
		case "ts":
			t = v
		case "h1":
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
	mac.Write([]byte(t + ":"))
	mac.Write(body)
	want := mac.Sum(nil)
	for _, s := range sigs {
		if got, err := hex.DecodeString(s); err == nil && hmac.Equal(got, want) {
			return true
		}
	}
	return false
}
