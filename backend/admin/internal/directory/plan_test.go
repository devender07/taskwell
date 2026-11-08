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

// TestSetPlan comps a user's account, clears the comp back to its active Paddle subscription's plan (read from a fake
// Paddle), then to Free once it has none; each change is in admin_audit and the audit log of every workspace the
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
	paddle := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/subscriptions/sub_1" || r.Header.Get("Authorization") != "Bearer pdl_key" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		fmt.Fprint(w, `{"data":{"id":"sub_1","items":[{"price":{"id":"pri_test","custom_data":{"taskwell_plan":"pro"}}}]}}`)
	}))
	defer paddle.Close()
	h := &Handler{model: &model{db: pool}, paddle: paddle.URL}
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

	if _, err := pool.Exec(ctx, `update users set paddle_subscription_id = 'sub_1', subscription_status = 'active' where id = $1`, id); err != nil {
		t.Fatal(err)
	}
	config := func(key, value string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `update system_config set value = $2 where key = $1`, key, value); err != nil {
			t.Fatal(err)
		}
	}
	config("paddle_api_key", "")
	set(url.Values{"comped": {"false"}}, 502) // billing off: the subscription's plan can't be read
	config("paddle_api_key", "pdl_key")
	defer config("paddle_api_key", "")
	set(url.Values{"comped": {"false"}}, 502) // paddle_environment isn't sandbox or production
	config("paddle_environment", "sandbox")
	defer config("paddle_environment", "")
	set(url.Values{"comped": {"false"}}, 200)
	if p, comped := plan(); p != "pro" || comped {
		t.Fatalf("cleared with a subscription: %s %v", p, comped)
	}

	if _, err := pool.Exec(ctx, `update users set paddle_subscription_id = null, subscription_status = null where id = $1`, id); err != nil {
		t.Fatal(err)
	}
	set(url.Values{"comped": {"false"}}, 200)
	if p, _ := plan(); p != "free" {
		t.Fatalf("cleared without one: %s", p)
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
