package directory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const stripeBase = "https://api.stripe.com"

var stripeClient = &http.Client{Timeout: 10 * time.Second}

// plans maps a Stripe price lookup key to its plan (the user service's plan package has the same).
var plans = map[string]string{"pro_monthly": "pro", "ultimate_monthly": "ultimate"}

// subscriptionPlan reads the plan of the Stripe subscription with the id, by its first item's price lookup key.
func subscriptionPlan(ctx context.Context, base, key, id string) (string, error) {
	if key == "" {
		return "", errors.New("stripe_secret_key is empty")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/subscriptions/"+url.PathEscape(id), nil)
	if err != nil {
		return "", fmt.Errorf("stripe request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Stripe-Version", "2026-09-30.endive")
	res, err := stripeClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("stripe subscription: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("stripe subscription: status %d", res.StatusCode)
	}
	var s struct {
		Items struct {
			Data []struct {
				Price struct {
					LookupKey string `json:"lookup_key"`
				} `json:"price"`
			} `json:"data"`
		} `json:"items"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&s); err != nil {
		return "", fmt.Errorf("stripe subscription: decode: %w", err)
	}
	if len(s.Items.Data) == 0 {
		return "", errors.New("stripe subscription: no items")
	}
	plan, ok := plans[s.Items.Data[0].Price.LookupKey]
	if !ok {
		return "", fmt.Errorf("stripe subscription: unknown lookup key %q", s.Items.Data[0].Price.LookupKey)
	}
	return plan, nil
}
