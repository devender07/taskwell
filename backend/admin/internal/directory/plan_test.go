package directory

import (
	"context"
	"crypto/rand"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestSetPlan comps a user's account, clears the comp back to the plan of its mirrored Paddle subscription granting
// access, then to Free once it's paused; each change is in admin_audit and the audit log of every workspace the
// user owns, and the workspaces list shows the owner's plan. Needs TEST_DATABASE_URL migrated by the user service's
// tests.
func TestSetPlan(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	email := "plan-" + strings.ToLower(rand.Text()[:8]) + "@dbtest.test"
	var id, ws1, ws2 string
	if err := pool.QueryRow(ctx, `insert into users (name, email) values ('Plan', $1) returning id::text`, email).Scan(&id); err != nil {
		t.Fatal(err)
	}
	for _, ws := range []*string{&ws1, &ws2} {
		if err := pool.QueryRow(ctx, `insert into workspaces (name, slug, created_by) values ('Plan', $1, $2) returning id::text`,
			"plan-"+strings.ToLower(rand.Text()[:8]), id).Scan(ws); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from workspaces where created_by = $1`, id)
		_, _ = pool.Exec(context.Background(), `delete from users where id = $1`, id)
		_, _ = pool.Exec(context.Background(), `delete from admin_audit where target like $1`, email+":%")
	})
	h := &Handler{model: &model{db: pool}}
	set := func(form url.Values, want int) string {
		t.Helper()
		r := httptest.NewRequest("POST", "/api/users/"+id+"/plan", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.SetPathValue("id", id)
		w := httptest.NewRecorder()
		if err := h.setPlan(w, r, ""); err != nil {
			t.Fatal(err)
		}
		if w.Code != want {
			t.Fatalf("%v: %d %s, want %d", form, w.Code, w.Body, want)
		}
		return w.Body.String()
	}
	plan := func() (p string, comped bool) {
		t.Helper()
		if err := pool.QueryRow(ctx, `select plan, plan_comped from users where id = $1`, id).Scan(&p, &comped); err != nil {
			t.Fatal(err)
		}
		return p, comped
	}

	set(url.Values{"plan": {"gold"}, "comped": {"true"}}, 400)
	set(url.Values{"plan": {"pro"}}, 400)
	set(url.Values{"plan": {"ultimate"}, "comped": {"true"}}, 400)
	if body := set(url.Values{"plan": {"advanced"}, "comped": {"true"}}, 200); !strings.Contains(body, `"plan":"advanced","comped":true`) {
		t.Fatalf("comp answered %s", body)
	}
	if w, err := scanWorkspace(pool.QueryRow(ctx, `select `+workspaceColumns+` from `+workspaceFrom+` where w.id = $1`, ws1)); err != nil || w.Plan != "advanced" || !w.Comped {
		t.Fatalf("workspace plan %q %v", w.Plan, err)
	}

	cus, sub := "ctm_"+email, "sub_"+email
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from paddle_subscriptions where subscription_id = $1`, sub)
		_, _ = pool.Exec(context.Background(), `delete from paddle_customers where customer_id = $1`, cus)
	})
	if _, err := pool.Exec(ctx, `insert into paddle_customers (customer_id, user_id) values ($1, $2)`, cus, id); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into paddle_subscriptions (subscription_id, customer_id, user_id, status, price_id, product_id, plan, paddle_updated_at)
		values ($2, $1, $3, 'past_due', 'pri_1', 'pro_1', 'pro', now())`, cus, sub, id); err != nil {
		t.Fatal(err)
	}
	set(url.Values{"comped": {"false"}}, 200)
	if p, comped := plan(); p != "pro" || comped {
		t.Fatalf("cleared with a subscription: %s %v", p, comped)
	}

	if _, err := pool.Exec(ctx, `update paddle_subscriptions set status = 'paused' where subscription_id = $1`, sub); err != nil {
		t.Fatal(err)
	}
	set(url.Values{"comped": {"false"}}, 200)
	if p, _ := plan(); p != "free" {
		t.Fatalf("cleared without one granting access: %s", p)
	}
	var admin, a1, a2 int
	if err := pool.QueryRow(ctx, `select (select count(*) from admin_audit where action = 'user.plan_set' and target like $3),
		(select count(*) from workspace_audit where workspace_id = $1 and action = 'billing.plan_changed'),
		(select count(*) from workspace_audit where workspace_id = $2 and action = 'billing.plan_changed')`, ws1, ws2, email+":%").
		Scan(&admin, &a1, &a2); err != nil || admin != 3 || a1 != 3 || a2 != 3 {
		t.Fatalf("audit: admin %d, workspaces %d %d, %v", admin, a1, a2, err)
	}
	set(url.Values{"comped": {"false"}}, 200) // no change: admin_audit only
}
