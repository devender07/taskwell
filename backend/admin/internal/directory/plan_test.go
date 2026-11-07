package directory

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestSetPlan comps a workspace, clears the comp back to its active Stripe subscription's plan (read from a fake
// Stripe), then to Starter once it has none; each change is in admin_audit and the workspace's audit log. Needs
// TEST_DATABASE_URL migrated by the user service's tests.
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
	slug := "plan-" + strings.ToLower(rand.Text()[:8])
	var id string
	if err := pool.QueryRow(ctx, `insert into workspaces (name, slug) values ('Plan', $1) returning id::text`, slug).Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from workspaces where id = $1`, id)
		_, _ = pool.Exec(context.Background(), `delete from admin_audit where target like $1`, slug+":%")
	})
	stripe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/subscriptions/sub_1" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		fmt.Fprint(w, `{"id":"sub_1","items":{"data":[{"price":{"lookup_key":"pro_monthly"}}]}}`)
	}))
	defer stripe.Close()
	h := &Handler{model: &model{db: pool}, stripe: stripe.URL}
	set := func(form url.Values, want int) string {
		t.Helper()
		r := httptest.NewRequest("POST", "/api/workspaces/"+id+"/plan", strings.NewReader(form.Encode()))
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
		if err := pool.QueryRow(ctx, `select plan, plan_comped from workspaces where id = $1`, id).Scan(&p, &comped); err != nil {
			t.Fatal(err)
		}
		return p, comped
	}

	set(url.Values{"plan": {"gold"}, "comped": {"true"}}, 400)
	set(url.Values{"plan": {"pro"}}, 400)
	if body := set(url.Values{"plan": {"ultimate"}, "comped": {"true"}}, 200); !strings.Contains(body, `"plan":"ultimate","comped":true`) {
		t.Fatalf("comp answered %s", body)
	}

	if _, err := pool.Exec(ctx, `update workspaces set stripe_subscription_id = 'sub_1', subscription_status = 'active' where id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `update system_config set value = '' where key = 'stripe_secret_key'`); err != nil {
		t.Fatal(err)
	}
	set(url.Values{"comped": {"false"}}, 502) // billing off: the subscription's plan can't be read
	if _, err := pool.Exec(ctx, `update system_config set value = 'rk_test' where key = 'stripe_secret_key'`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `update system_config set value = '' where key = 'stripe_secret_key'`)
	})
	set(url.Values{"comped": {"false"}}, 200)
	if p, comped := plan(); p != "pro" || comped {
		t.Fatalf("cleared with a subscription: %s %v", p, comped)
	}

	if _, err := pool.Exec(ctx, `update workspaces set stripe_subscription_id = null, subscription_status = null where id = $1`, id); err != nil {
		t.Fatal(err)
	}
	set(url.Values{"comped": {"false"}}, 200)
	if p, _ := plan(); p != "starter" {
		t.Fatalf("cleared without one: %s", p)
	}
	var admin, ws int
	if err := pool.QueryRow(ctx, `select (select count(*) from admin_audit where action = 'workspace.plan_set' and target like $2),
		(select count(*) from workspace_audit where workspace_id = $1 and action = 'billing.plan_changed')`, id, slug+":%").
		Scan(&admin, &ws); err != nil || admin != 3 || ws != 3 {
		t.Fatalf("audit: admin %d, workspace %d, %v", admin, ws, err)
	}
	set(url.Values{"comped": {"false"}}, 200) // no change: admin_audit only
}
