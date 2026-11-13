package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/mail"
	"github.com/devender07/taskwell/backend/user/internal/secret"
	"github.com/devender07/taskwell/backend/user/internal/sysconfig"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
	"github.com/devender07/taskwell/backend/user/internal/wsaudit"
)

// testAPI is the auth API on a test database, with a user who has a password.
type testAPI struct {
	t      *testing.T
	pool   *pgxpool.Pool
	h      *Handler
	mux    *http.ServeMux
	userID string
	email  string
}

const testPassword = "correct horse"

func newTestAPI(t *testing.T) *testAPI {
	pool := dbtest.Pool(t)
	cfg := sysconfig.New(pool)
	h := New(pool, cfg, mail.New(pool, cfg, time.Second, time.Second, 1), []byte("compliance-test-secret-at-least-32-bytes"), nil)
	mux := http.NewServeMux()
	h.Register(mux)
	a := &testAPI{t: t, pool: pool, h: h, mux: mux, userID: dbtest.User(t, pool, "Ann")}
	ph, _ := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.MinCost)
	if err := pool.QueryRow(context.Background(), `update users set password_hash = $2 where id = $1 returning email`,
		a.userID, string(ph)).Scan(&a.email); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from auth_failures where key in ('pw:' || $1, 'code:' || $1)`, a.email)
	})
	return a
}

func (a *testAPI) do(method, path string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	a.t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("User-Agent", "compliance-test")
	r.RemoteAddr = fmt.Sprintf("203.0.113.%d:1234", rand.IntN(250)+1) // spread over the per-IP limit
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	a.mux.ServeHTTP(w, r)
	return w
}

// signin signs the user in and returns the access cookie.
func (a *testAPI) signin() *http.Cookie {
	a.t.Helper()
	w := a.do("POST", "/api/auth/signin", url.Values{"email": {a.email}, "password": {testPassword}})
	for _, c := range w.Result().Cookies() {
		if c.Name == accessCookie && c.MaxAge >= 0 {
			return c
		}
	}
	a.t.Fatalf("sign-in: %d %s", w.Code, w.Body)
	return nil
}

func TestSignupNeedsTerms(t *testing.T) {
	a := newTestAPI(t)
	email := dbtest.Name("terms-") + "@dbtest.test"
	t.Cleanup(func() { _, _ = a.pool.Exec(context.Background(), `delete from users where email = $1`, email) })
	form := url.Values{"name": {"Ada"}, "email": {email}, "password": {testPassword}}
	w := a.do("POST", "/api/auth/signup", form)
	if w.Code != 400 || !strings.Contains(w.Body.String(), `"accept_terms":"Confirm you're 18 or older and accept the terms."`) {
		t.Fatalf("without accept_terms: %d %s", w.Code, w.Body)
	}
	form.Set("accept_terms", "true")
	if w := a.do("POST", "/api/auth/signup", form); strings.Contains(w.Body.String(), "accept_terms") {
		t.Fatalf("with accept_terms: %d %s", w.Code, w.Body)
	}
}

// TestAccountEvents: a wrong password and a sign-in are recorded, listed newest first, only to their owner.
func TestAccountEvents(t *testing.T) {
	a := newTestAPI(t)
	if w := a.do("POST", "/api/auth/signin", url.Values{"email": {a.email}, "password": {"wrong password"}}); w.Code != 401 {
		t.Fatalf("wrong password: %d", w.Code)
	}
	access := a.signin()
	w := a.do("GET", "/api/me/account-events", nil, access)
	var got struct {
		Events []AccountEvent `json:"events"`
		More   bool           `json:"has_more"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || w.Code != 200 {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}
	if len(got.Events) != 2 || got.More || got.Events[0].Action != "sign_in" || got.Events[0].Detail != "password" ||
		got.Events[1].Action != "sign_in_failed" || got.Events[0].IP == nil || got.Events[0].UserAgent != "compliance-test" {
		t.Fatalf("events: %+v", got)
	}
	if w := a.do("GET", "/api/me/account-events?before="+got.Events[0].ID, nil, access); !strings.Contains(w.Body.String(), `"sign_in_failed"`) ||
		strings.Contains(w.Body.String(), `"sign_in"`) {
		t.Fatalf("older page: %s", w.Body)
	}
	other := newTestAPI(t)
	if w := other.do("GET", "/api/me/account-events", nil, other.signin()); strings.Contains(w.Body.String(), "sign_in_failed") {
		t.Fatalf("someone else's events: %s", w.Body)
	}
}

func TestExport(t *testing.T) {
	a := newTestAPI(t)
	w := a.do("GET", "/api/me/export", nil, a.signin())
	if w.Code != 200 || w.Header().Get("Content-Disposition") != `attachment; filename="taskwell-my-data.json"` {
		t.Fatalf("export: %d %v %s", w.Code, w.Header(), w.Body)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"exported_at", "profile", "workspaces", "notification_settings", "api_tokens", "sessions", "account_events"} {
		if _, ok := got[k]; !ok {
			t.Errorf("no %s", k)
		}
	}
	var p map[string]any
	if err := json.Unmarshal(got["profile"], &p); err != nil || p["email"] != a.email || p["has_password"] != true || len(p) != 8 {
		t.Fatalf("profile: %v %v", p, err)
	}
	if b := w.Body.String(); strings.Contains(b, "$2a$") || strings.Contains(b, "hash") || !strings.Contains(b, `"sign_in"`) {
		t.Fatalf("export leaks a hash or misses the sign-in: %s", b)
	}
}

// TestDeleteAccount: deleting is refused while the user is the only admin of a shared workspace and while their plan
// is billed, takes the code, then deletes the account with its solo workspace and keeps shared content, authorless.
func TestDeleteAccount(t *testing.T) {
	a := newTestAPI(t)
	ctx := context.Background()
	pool := a.pool
	bob := dbtest.User(t, pool, "Bob")
	var shared, solo, sharedSlug string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, a.userID, "Ann", "Shared"); err != nil {
			return err
		}
		if err := workspace.CreateFirst(ctx, tx, a.userID, "Ann", "Solo"); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `select (select id::text from workspaces where created_by = $1 and name = 'Shared'),
			(select slug from workspaces where created_by = $1 and name = 'Shared'),
			(select id::text from workspaces where created_by = $1 and name = 'Solo')`, a.userID).Scan(&shared, &sharedSlug, &solo)
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `delete from workspaces where id = $1`, shared) }) // its creator is gone
	dbtest.Join(t, pool, shared, bob, "member")
	var comment string
	if err := pool.QueryRow(ctx, `
		with p as (insert into projects (workspace_id, name, key) values ($1, 'P', 'DEL') returning id),
		     i as (insert into issues_all (workspace_id, project_id, number, title, status_id, rank)
		           select $1, p.id, 1, 'x', (select id from statuses where workspace_id = $1 limit 1), '1V' from p returning id)
		insert into comments (issue_id, author_id, body) select id, $2, '{}' from i returning id::text`, shared, a.userID).Scan(&comment); err != nil {
		t.Fatal(err)
	}
	for _, ws := range []string{shared, solo} {
		if err := wsaudit.Record(ctx, pool, ws, a.userID, "role.deleted", "Triage", nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.h.model.replaceToken(ctx, a.userID, purposeAccountDelete, secret.Hash("123456"), 10); err != nil {
		t.Fatal(err)
	}
	access := a.signin()
	del := func(code string) *httptest.ResponseRecorder {
		return a.do("POST", "/api/me/delete", url.Values{"code": {code}}, access)
	}

	if w := del("123456"); w.Code != 409 || !strings.Contains(w.Body.String(), "Make someone else an admin") ||
		!strings.Contains(w.Body.String(), `"slug":"`+sharedSlug+`"`) {
		t.Fatalf("only admin of a shared workspace: %d %s", w.Code, w.Body)
	}
	if _, err := pool.Exec(ctx, `update workspace_members set role_id = (select id from workspace_roles where workspace_id = $1 and builtin = 'admin')
		where workspace_id = $1 and user_id = $2`, shared, bob); err != nil {
		t.Fatal(err)
	}
	customer, sub := dbtest.Name("ctm_"), dbtest.Name("sub_")
	if _, err := pool.Exec(ctx, `
		with c as (insert into paddle_customers (customer_id, user_id) values ($1, $3) returning customer_id)
		insert into paddle_subscriptions (subscription_id, customer_id, user_id, status, price_id, product_id, plan, paddle_updated_at)
		select $2, customer_id, $3, 'active', 'pri_x', 'pro_x', 'pro', now() from c`, customer, sub, a.userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `delete from paddle_subscriptions where subscription_id = $1`, sub)
		_, _ = pool.Exec(ctx, `delete from paddle_customers where customer_id = $1`, customer)
	})
	if w := del("123456"); w.Code != 409 || !strings.Contains(w.Body.String(), "Cancel the plan") || !strings.Contains(w.Body.String(), `"name":"Solo"`) {
		t.Fatalf("paid plan: %d %s", w.Code, w.Body)
	}
	if _, err := pool.Exec(ctx, `update paddle_subscriptions set scheduled_change_action = 'cancel' where subscription_id = $1`, sub); err != nil {
		t.Fatal(err)
	}
	if w := del("000000"); w.Code != 400 || !strings.Contains(w.Body.String(), "That code is wrong or has expired.") {
		t.Fatalf("wrong code: %d %s", w.Code, w.Body)
	}

	w := del("123456")
	if w.Code != 204 {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	cleared := false
	for _, c := range w.Result().Cookies() {
		cleared = cleared || (c.Name == accessCookie && c.MaxAge < 0)
	}
	if !cleared {
		t.Error("the access cookie stays")
	}
	var users, solos, mails int
	var author, actor *string
	if err := pool.QueryRow(ctx, `select (select count(*) from users where id = $1), (select count(*) from workspaces where id = $2),
		(select author_id::text from comments where id = $3), (select actor_id::text from workspace_audit where workspace_id = $4 and action = 'role.deleted'),
		(select count(*) from outbound_mail where recipient = $5 and subject = 'Your Taskwell account was deleted')`,
		a.userID, solo, comment, shared, a.email).Scan(&users, &solos, &author, &actor, &mails); err != nil {
		t.Fatal(err)
	}
	if users != 0 || solos != 0 || author != nil || actor != nil || mails != 1 {
		t.Fatalf("after delete: users %d, solo workspaces %d, comment author %v, audit actor %v, mails %d", users, solos, author, actor, mails)
	}
	if w := a.do("GET", "/api/me", nil, access); w.Code != 401 {
		t.Fatalf("signed in after delete: %d", w.Code)
	}
}
