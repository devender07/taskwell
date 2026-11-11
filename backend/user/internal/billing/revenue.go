package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// amount parses a Paddle amount: a string of minor units.
func amount(s string) (int64, error) {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("amount %q: %w", s, err)
	}
	return n, nil
}

// saveTransaction mirrors a completed or paid transaction (any other status is skipped): its details.totals, billed
// at billed_at (its created_at while Paddle sends none).
func (h *Handler) saveTransaction(ctx context.Context, t transaction) error {
	if t.Status != "completed" && t.Status != "paid" {
		return nil
	}
	tt := t.Details.Totals
	if t.ID == "" || t.UpdatedAt.IsZero() || tt.CurrencyCode == "" {
		return fmt.Errorf("transaction %q: no id, updated_at or details.totals.currency_code", t.ID)
	}
	row := transactionRow{ID: t.ID, CustomerID: t.CustomerID, SubscriptionID: t.SubscriptionID, Status: t.Status,
		Currency: strings.ToLower(tt.CurrencyCode), BilledAt: t.CreatedAt, UpdatedAt: t.UpdatedAt}
	if t.BilledAt != nil {
		row.BilledAt = *t.BilledAt
	}
	var err error
	for _, f := range []struct {
		into *int64
		s    string
	}{{&row.Subtotal, tt.Subtotal}, {&row.Tax, tt.Tax}, {&row.Total, tt.Total}} {
		if *f.into, err = amount(f.s); err != nil {
			return fmt.Errorf("transaction %s: %w", t.ID, err)
		}
	}
	if tt.Earnings != nil {
		n, err := amount(*tt.Earnings)
		if err != nil {
			return fmt.Errorf("transaction %s earnings: %w", t.ID, err)
		}
		row.Earnings = &n
	}
	return h.model.saveTransaction(ctx, row, h.link(t.CustomData))
}

// saveAdjustment mirrors an adjustment: its totals.total in totals.currency_code.
func (h *Handler) saveAdjustment(ctx context.Context, a adjustment) error {
	cur := a.Totals.CurrencyCode
	if cur == "" {
		cur = a.CurrencyCode
	}
	if a.ID == "" || a.UpdatedAt.IsZero() || cur == "" {
		return fmt.Errorf("adjustment %q: no id, updated_at or currency_code", a.ID)
	}
	total, err := amount(a.Totals.Total)
	if err != nil {
		return fmt.Errorf("adjustment %s: %w", a.ID, err)
	}
	created := a.CreatedAt
	if created.IsZero() {
		created = a.UpdatedAt
	}
	return h.model.saveAdjustment(ctx, adjustmentRow{ID: a.ID, TransactionID: a.TransactionID, CustomerID: a.CustomerID,
		Action: a.Action, Status: a.Status, Currency: strings.ToLower(cur), Total: total, CreatedAt: created, UpdatedAt: a.UpdatedAt})
}

// BackfillCounts is what Backfill stored.
type BackfillCounts struct{ Transactions, Adjustments, Subscriptions int64 }

// Backfill copies what Paddle has from before the webhooks were stored: every completed or paid transaction, every
// adjustment, and the unit amount of each mirrored subscription's price that has none. It upserts like the
// webhooks, so it's safe to run again. ponytail: a subscription gets its price's amount today, not the one it was
// subscribed at; read /subscriptions instead if prices are ever repriced in place.
func (h *Handler) Backfill(ctx context.Context) (BackfillCounts, error) {
	var c BackfillCounts
	s, err := h.settings(ctx)
	if err != nil {
		return c, err
	}
	if err := h.paddle.list(ctx, s.key, s.env, "/transactions?status=completed,paid&per_page=30", func(b json.RawMessage) error {
		var t transaction
		if err := json.Unmarshal(b, &t); err != nil {
			return fmt.Errorf("decode transaction: %w", err)
		}
		c.Transactions++
		return h.saveTransaction(ctx, t)
	}); err != nil {
		return c, fmt.Errorf("backfill transactions: %w", err)
	}
	if err := h.paddle.list(ctx, s.key, s.env, "/adjustments?per_page=50", func(b json.RawMessage) error {
		var a adjustment
		if err := json.Unmarshal(b, &a); err != nil {
			return fmt.Errorf("decode adjustment: %w", err)
		}
		c.Adjustments++
		return h.saveAdjustment(ctx, a)
	}); err != nil {
		return c, fmt.Errorf("backfill adjustments: %w", err)
	}
	ids, err := h.model.pricesWithoutAmount(ctx)
	if err != nil {
		return c, err
	}
	for _, id := range ids {
		var pr price
		var pe *paddleError
		err := h.paddle.call(ctx, s.key, s.env, http.MethodGet, "/prices/"+url.PathEscape(id), nil, &pr)
		if errors.As(err, &pe) && pe.status == http.StatusNotFound {
			slog.WarnContext(ctx, "paddle-backfill: no such price, its subscriptions keep no unit amount", "price", id)
			continue
		}
		if err != nil {
			return c, fmt.Errorf("backfill price %s: %w", id, err)
		}
		n, err := amount(pr.UnitPrice.Amount)
		if err != nil {
			return c, fmt.Errorf("backfill price %s: %w", id, err)
		}
		set, err := h.model.setUnitAmount(ctx, id, n)
		if err != nil {
			return c, err
		}
		c.Subscriptions += set
	}
	return c, nil
}
