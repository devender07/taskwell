package directory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"time"
)

var paddleClient = &http.Client{Timeout: 10 * time.Second}

// paidPlans are the user API's paid plan ids (its package plan), free is the plan without a subscription.
var paidPlans = []string{"starter", "pro", "advanced"}

const freePlan = "free"

// paddleConfig is the user API's paddle_ config.
type paddleConfig struct{ env, key string }

// subscriptionPlan reads the plan of the Paddle subscription with the id: its first item's price's
// custom_data.taskwell_plan, as the user API does. base, when not "", overrides the API base paddle_environment picks.
func subscriptionPlan(ctx context.Context, base string, c paddleConfig, id string) (string, error) {
	if c.key == "" {
		return "", errors.New("paddle_api_key is empty")
	}
	if c.env != "sandbox" && c.env != "production" {
		return "", fmt.Errorf("paddle_environment %q isn't sandbox or production", c.env)
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
					ID         string `json:"id"`
					CustomData struct {
						Plan string `json:"taskwell_plan"`
					} `json:"custom_data"`
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
	p := s.Data.Items[0].Price
	if !slices.Contains(paidPlans, p.CustomData.Plan) {
		return "", fmt.Errorf("paddle subscription: price %q has no known custom_data.taskwell_plan (%q)", p.ID, p.CustomData.Plan)
	}
	return p.CustomData.Plan, nil
}
