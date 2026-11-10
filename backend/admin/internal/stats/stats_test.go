package stats

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestStats seeds a Pro user with a past_due subscription, a comp ending in 3 days and a sign-in, and checks the
// overview has every field, 30 days per chart and counts that include them. Needs TEST_DATABASE_URL migrated by the
// user service's tests.
func TestStats(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	email := "stats-" + strings.ToLower(rand.Text()[:8]) + "@dbtest.test"
	cus, sub := "ctm_"+email, "sub_"+email
	var id string
	if err := pool.QueryRow(ctx, `insert into users (name, email, plan, plan_comped, plan_comped_until)
		values ('Stats', $1, 'pro', true, now() + interval '3 days') returning id::text`, email).Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `delete from paddle_adjustments where transaction_id = $1`, "txn_"+email)
		_, _ = pool.Exec(c, `delete from paddle_transactions where customer_id = $1`, cus)
		_, _ = pool.Exec(c, `delete from paddle_subscriptions where customer_id = $1`, cus)
		_, _ = pool.Exec(c, `delete from paddle_customers where customer_id = $1`, cus)
		_, _ = pool.Exec(c, `delete from refresh_tokens where user_id = $1`, id)
		_, _ = pool.Exec(c, `delete from users where id = $1`, id)
	})
	if _, err := pool.Exec(ctx, `insert into paddle_customers (customer_id, user_id) values ($1, $2)`, cus, id); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into paddle_subscriptions (subscription_id, customer_id, user_id, status, price_id,
		product_id, plan, billing_interval, paddle_updated_at, currency_code, unit_amount)
		values ($2, $1, $3, 'past_due', 'pri_1', 'pro_1', 'pro', 'month', now(), 'xts', 1200),
		       ($2||'y', $1, $3, 'active', 'pri_2', 'pro_1', 'pro', 'year', now(), 'xts', 12000),
		       ($2||'t', $1, $3, 'trialing', 'pri_1', 'pro_1', 'pro', 'month', now(), 'xts', 1200)`,
		cus, sub, id); err != nil {
		t.Fatal(err)
	}
	// Revenue in the ISO test currency: today's payment, one 40 days ago, an approved refund and chargeback and a
	// pending refund.
	txn := "txn_" + email
	if _, err := pool.Exec(ctx, `insert into paddle_transactions (transaction_id, user_id, customer_id, status, currency_code,
		subtotal, tax, total, earnings, billed_at, paddle_updated_at)
		values ($1, $2, $3, 'completed', 'xts', 1250, 250, 1500, 1400, now(), now()),
		       ($1||'old', $2, $3, 'completed', 'xts', 400, 100, 500, null, now() - interval '40 days', now())`, txn, id, cus); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into paddle_adjustments (adjustment_id, transaction_id, user_id, action, status, currency_code,
		total, created_at, paddle_updated_at)
		values ($1||'r', $1, $2, 'refund', 'approved', 'xts', 300, now(), now()),
		       ($1||'c', $1, $2, 'chargeback', 'approved', 'xts', 200, now(), now()),
		       ($1||'p', $1, $2, 'refund', 'pending_approval', 'xts', 999, now(), now())`, txn, id); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into refresh_tokens (token_hash, user_id, expires_at, family_id)
		values (convert_to($1, 'utf8'), $2, now() + interval '1 day', uuidv7())`, email, id); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	h := &Handler{model: &model{db: pool}}
	if err := h.get(w, httptest.NewRequest("GET", "/api/stats", nil), ""); err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("%d %s: %v", w.Code, w.Body, err)
	}
	for _, k := range []string{"totals", "active_users_7d", "active_users_30d", "signups_7d", "suspended_users", "mfa_users",
		"plans", "billing", "subscriptions", "comps_ending", "recent_signups", "top_workspaces", "health",
		"signups", "issues_created", "active_users", "workspaces_created", "revenue"} {
		if v, ok := raw[k]; !ok || string(v) == "null" {
			t.Errorf("missing %s", k)
		}
	}
	var s Stats
	if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	for name, days := range map[string][]Day{"signups": s.Signups, "issues_created": s.IssuesCreated,
		"active_users": s.ActiveUsers, "workspaces_created": s.WorkspacesCreated} {
		if len(days) != 30 {
			t.Errorf("%s has %d days", name, len(days))
		}
	}
	if s.Signups[29].Count < 1 || s.ActiveUsers[29].Count < 1 || s.Signups7d < 1 || s.ActiveUsers7d < 1 {
		t.Errorf("today: signups %d, active %d, 7d %d %d", s.Signups[29].Count, s.ActiveUsers[29].Count, s.Signups7d, s.ActiveUsers7d)
	}
	b := s.Billing
	if s.Plans.Pro < 1 || b.Paying < 1 || b.PastDue < 1 || b.Monthly < 1 || b.Comped < 1 || b.CompsEnding7d < 1 {
		t.Errorf("plans %+v, billing %+v", s.Plans, b)
	}
	found := false
	for _, c := range s.CompsEnding {
		found = found || c.UserID == id
	}
	if !found {
		t.Errorf("comps_ending %+v lacks the seeded user", s.CompsEnding)
	}
	if len(s.Subscriptions) == 0 || len(s.RecentSignups) == 0 {
		t.Errorf("subscriptions %d, recent signups %d", len(s.Subscriptions), len(s.RecentSignups))
	}
	var xts *Currency
	for i, c := range s.Revenue.Currencies {
		if c.Currency == "xts" {
			xts = &s.Revenue.Currencies[i]
		}
	}
	// MRR: past_due 1200 + yearly 12000/12; the trial isn't counted.
	if xts == nil || xts.MRR != 2200 || xts.Revenue30d != 1500 || xts.Earnings30d != 1400 || xts.Refunds30d != 300 ||
		xts.Chargebacks30d != 200 || xts.RevenueAll != 2000 || len(xts.PerDay) != 30 || xts.PerDay[29].Count != 1500 {
		t.Errorf("revenue xts: %+v", xts)
	}
	found = false
	for _, p := range s.Revenue.Recent {
		found = found || (p.TransactionID == txn && p.UserID == id && p.Email == email && p.Total == 1500 && p.Currency == "xts")
	}
	if !found {
		t.Errorf("recent payments %+v lack the seeded one", s.Revenue.Recent)
	}
}
