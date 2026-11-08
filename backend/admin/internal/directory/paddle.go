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

var paddleClient = &http.Client{Timeout: 10 * time.Second}

// paddleConfig is the user API's paddle_ config.
type paddleConfig struct {
	env, key string
	prices   map[string]string // plan id by price id
}

// subscriptionPlan reads the plan of the Paddle subscription with the id, by its first item's price. base, when not
// "", overrides the API base paddle_environment picks.
func subscriptionPlan(ctx context.Context, base string, c paddleConfig, id string) (string, error) {
	if c.key == "" {
		return "", errors.New("paddle_api_key is empty")
	}
	if base == "" {
		base = "https://sandbox-api.paddle.com"
		if c.env == "production" {
			base = "https://api.paddle.com"
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/subscriptions/"+url.PathEscape(id), nil)
	if err != nil {
		return "", fmt.Errorf("paddle request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	res, err := paddleClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("paddle subscription: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("paddle subscription: status %d", res.StatusCode)
	}
	var s struct {
		Data struct {
			Items []struct {
				Price struct {
					ID string `json:"id"`
				} `json:"price"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&s); err != nil {
		return "", fmt.Errorf("paddle subscription: decode: %w", err)
	}
	if len(s.Data.Items) == 0 {
		return "", errors.New("paddle subscription: no items")
	}
	plan, ok := c.prices[s.Data.Items[0].Price.ID]
	if !ok || s.Data.Items[0].Price.ID == "" {
		return "", fmt.Errorf("paddle subscription: unknown price %q", s.Data.Items[0].Price.ID)
	}
	return plan, nil
}
