package directory

import (
	"context"
	"crypto/rand"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestUserDetail seeds a user with a workspace, two-factor, a subscription, a payment, an account event and a comp, and
// checks GET /api/users/{id} has every section (the comp once, though it's in admin_audit and the workspace's audit),
// unknown ids are 404, and GET /api/users.csv has the header and escapes formulas. Needs TEST_DATABASE_URL migrated by
// the user service's tests.
func TestUserDetail(t *testing.T) {
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
	email := "detail-" + strings.ToLower(rand.Text()[:8]) + "@dbtest.test"
	cus := "ctm_" + email
	var id, wsID string
	if err := pool.QueryRow(ctx, `insert into users (name, email) values ('=1+2', $1) returning id::text`, email).Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `delete from paddle_transactions where customer_id = $1`, cus)
		_, _ = pool.Exec(c, `delete from paddle_subscriptions where customer_id = $1`, cus)
		_, _ = pool.Exec(c, `delete from paddle_customers where customer_id = $1`, cus)
		_, _ = pool.Exec(c, `delete from workspaces where created_by = $1`, id)
		_, _ = pool.Exec(c, `delete from users where id = $1`, id)
	})
	if err := pool.QueryRow(ctx, `insert into workspaces (name, slug, created_by) values ('Detail', $1, $2) returning id::text`,
		"detail-"+strings.ToLower(rand.Text()[:8]), id).Scan(&wsID); err != nil {
		t.Fatal(err)
	}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`insert into workspace_roles (workspace_id, name, builtin, permissions) values ($1, 'Admin', 'admin', '{}')`, []any{wsID}},
		{`insert into workspace_members (workspace_id, user_id, role_id) select $1, $2, id from workspace_roles where workspace_id = $1`,
			[]any{wsID, id}},
		{`insert into user_mfa (user_id, secret, recovery_codes, enabled_at) values ($1, 'x', '{}', now())`, []any{id}},
		{`insert into account_events (user_id, action, detail, ip) values ($1, 'sign_in', 'password', '203.0.113.9')`, []any{id}},
		{`insert into paddle_customers (customer_id, user_id) values ($1, $2)`, []any{cus, id}},
		{`insert into paddle_subscriptions (subscription_id, customer_id, user_id, status, price_id, product_id, plan,
			billing_interval, paddle_updated_at, currency_code, unit_amount)
			values ('sub_' || $1, $1, $2, 'active', 'pri_1', 'pro_1', 'pro', 'month', now(), 'usd', 1200)`, []any{cus, id}},
		{`insert into paddle_transactions (transaction_id, user_id, customer_id, status, currency_code, subtotal, tax, total,
			billed_at, paddle_updated_at) values ('txn_' || $1, $2, $1, 'completed', 'usd', 1000, 200, 1200, now(), now())`, []any{cus, id}},
	} {
		if _, err := pool.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatalf("%s: %v", q.sql, err)
		}
	}
	h := &Handler{model: &model{db: pool}}
	plan := httptest.NewRequest("POST", "/api/users/"+id+"/plan", strings.NewReader("plan=pro&comped=true"))
	plan.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	plan.SetPathValue("id", id)
	if w := httptest.NewRecorder(); h.setPlan(w, plan, "") != nil || w.Code != 200 {
		t.Fatalf("comp: %d %s", w.Code, w.Body)
	}

	get := func(fn func(http.ResponseWriter, *http.Request, string) error, path, pathID string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", path, nil)
		r.SetPathValue("id", pathID)
		w := httptest.NewRecorder()
		if err := fn(w, r, ""); err != nil {
			t.Fatal(err)
		}
		return w
	}
	w := get(h.user, "/api/users/"+id, id)
	var d struct {
		User struct {
			Email string `json:"email"`
		} `json:"user"`
		MFA           bool              `json:"mfa"`
		Workspaces    []UserWorkspace   `json:"workspaces"`
		Subscriptions []json.RawMessage `json:"subscriptions"`
		Payments      []UserPayment     `json:"payments"`
		PlanHistory   []PlanChange      `json:"plan_history"`
		Events        []UserEvent       `json:"events"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil || w.Code != 200 {
		t.Fatalf("detail: %d %s", w.Code, w.Body)
	}
	switch {
	case d.User.Email != email || !d.MFA:
		t.Fatalf("user: %s", w.Body)
	case len(d.Workspaces) != 1 || !d.Workspaces[0].Owner || d.Workspaces[0].Role != "Admin" || d.Workspaces[0].Members != 1:
		t.Fatalf("workspaces: %+v", d.Workspaces)
	case len(d.Subscriptions) != 1 || !strings.Contains(string(d.Subscriptions[0]), `"unit_amount":1200`):
		t.Fatalf("subscriptions: %s", w.Body)
	case len(d.Payments) != 1 || d.Payments[0].Total != 1200:
		t.Fatalf("payments: %+v", d.Payments)
	case len(d.PlanHistory) != 1 || d.PlanHistory[0].Action != "user.plan_set" || d.PlanHistory[0].Detail != email+": pro (comped)":
		t.Fatalf("plan history: %+v", d.PlanHistory)
	case len(d.Events) != 1 || d.Events[0].IP == nil || *d.Events[0].IP != "203.0.113.9":
		t.Fatalf("events: %+v", d.Events)
	}
	for _, missing := range []string{"00000000-0000-0000-0000-000000000000", "nope"} {
		if w := get(h.user, "/api/users/"+missing, missing); w.Code != 404 || !strings.Contains(w.Body.String(), "User not found.") {
			t.Fatalf("%s: %d %s", missing, w.Code, w.Body)
		}
	}

	w = get(h.usersCSV, "/api/users.csv?q="+url.QueryEscape(email), "")
	if w.Header().Get("Content-Disposition") != `attachment; filename="users.csv"` || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("headers: %v", w.Header())
	}
	rows, err := csv.NewReader(w.Body).ReadAll()
	if err != nil || len(rows) != 2 {
		t.Fatalf("csv: %v %q", err, rows)
	}
	if strings.Join(rows[0], ",") != "id,name,email,plan,comped,comped_until,suspended,mfa,workspaces,created_at,last_active_at" ||
		rows[1][0] != id || rows[1][1] != "'=1+2" || rows[1][3] != "pro" || rows[1][7] != "true" {
		t.Fatalf("csv rows: %q", rows)
	}
	var exported int
	if err := pool.QueryRow(ctx, `select count(*) from admin_audit where action = 'users.exported' and target = $1`, email).
		Scan(&exported); err != nil || exported != 1 {
		t.Fatalf("export audited %d times: %v", exported, err)
	}
}
