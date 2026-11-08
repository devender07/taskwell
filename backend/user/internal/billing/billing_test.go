package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/sysconfig"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

const testSecret = "pdl_ntfset_test"

func sign(secret string, ts int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d:%s", ts, body)
	return fmt.Sprintf("ts=%d;h1=%s", ts, hex.EncodeToString(mac.Sum(nil)))
}

func TestVerifySignature(t *testing.T) {
	body := []byte(`{"event_id":"evt_1"}`)
	now := time.Unix(1_800_000_000, 0)
	good := sign(testSecret, now.Unix(), body)
	for _, c := range []struct {
		name, header string
		body         []byte
		want         bool
	}{
		{"good", good, body, true},
		{"second h1 matches", fmt.Sprintf("ts=%d;h1=00ff;%s", now.Unix(), strings.Split(good, ";")[1]), body, true},
		{"wrong secret", sign("pdl_other", now.Unix(), body), body, false},
		{"tampered body", good, []byte(`{"event_id":"evt_2"}`), false},
		{"stale", sign(testSecret, now.Add(-6*time.Minute).Unix(), body), body, false},
		{"future", sign(testSecret, now.Add(6*time.Minute).Unix(), body), body, false},
		{"only h0", strings.Replace(good, "h1=", "h0=", 1), body, false},
		{"empty", "", body, false},
	} {
		if got := verifySignature(c.header, c.body, testSecret, now); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	if verifySignature(good, body, "", now) {
		t.Error("an empty secret verified")
	}
}

// fakePaddle answers the Paddle calls billing makes and records each request.
type fakePaddle struct {
	mu   sync.Mutex
	subs map[string]string // subscription id → its JSON
	reqs []paddleReq
}

type paddleReq struct {
	method, path string
	body         map[string]any
}

func (f *fakePaddle) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer pdl_key" {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"error":{"type":"request_error","code":"forbidden","detail":"bad key"}}`)
		return
	}
	var body map[string]any
	b, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(b, &body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, paddleReq{r.Method, r.URL.Path, body})
	reply := func(data string) { fmt.Fprintf(w, `{"data":%s,"meta":{"request_id":"r1"}}`, data) }
	switch p := r.URL.Path; {
	case p == "/prices/pri_pro":
		reply(`{"id":"pri_pro","unit_price":{"amount":"1200","currency_code":"USD"},
			"unit_price_overrides":[{"country_codes":["IN"],"unit_price":{"amount":"69900","currency_code":"INR"}}]}`)
	case p == "/prices/pri_ult":
		reply(`{"id":"pri_ult","unit_price":{"amount":"4900","currency_code":"USD"},
			"unit_price_overrides":[{"country_codes":["GB","IN"],"unit_price":{"amount":"249900","currency_code":"INR"}}]}`)
	case p == "/transactions":
		reply(`{"id":"txn_1","status":"ready"}`)
	case strings.HasPrefix(p, "/customers/") && strings.HasSuffix(p, "/portal-sessions"):
		reply(`{"id":"cpls_1","urls":{"general":{"overview":"https://customer-portal.paddle.test/1"},"subscriptions":[]}}`)
	case strings.HasPrefix(p, "/subscriptions/") && f.subs[strings.TrimPrefix(p, "/subscriptions/")] != "":
		id := strings.TrimPrefix(p, "/subscriptions/")
		if r.Method == http.MethodPatch {
			items := body["items"].([]any)
			f.subs[id] = strings.Replace(f.subs[id], `"pri_pro"`, fmt.Sprintf("%q", items[0].(map[string]any)["price_id"]), 1)
		}
		reply(f.subs[id])
	default:
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":{"type":"request_error","code":"not_found","detail":"no such entity"}}`)
	}
}

func (f *fakePaddle) last(method, path string) (paddleReq, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	var last paddleReq
	for _, r := range f.reqs {
		if r.method == method && r.path == path {
			last, n = r, n+1
		}
	}
	return last, n
}

func subJSON(id, userID, status, priceID string, cancel bool) string {
	sc := "null"
	if cancel {
		sc = `{"action":"cancel","effective_at":"2030-03-17T17:46:40Z"}`
	}
	cd := "null"
	if userID != "" {
		cd = fmt.Sprintf(`{"user_id":%q}`, userID)
	}
	return fmt.Sprintf(`{"id":%q,"status":%q,"customer_id":"ctm_1","currency_code":"INR",
		"next_billed_at":"2030-03-17T17:46:40Z","scheduled_change":%s,"custom_data":%s,
		"items":[{"status":"active","quantity":1,"price":{"id":%q}}]}`, id, status, sc, cd, priceID)
}

// setup makes Ann's workspace with Bob as a plain member, and a billing handler against a fake Paddle whose config
// is cfg (every paddle_ key and app_base_url set).
func setup(t *testing.T) (pool *pgxpool.Pool, mux *http.ServeMux, f *fakePaddle, cfg map[string]string, wsID, slug, ann, bob string) {
	t.Helper()
	pool = dbtest.Pool(t)
	ctx := context.Background()
	ann, bob = dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Bob")
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `select id::text, slug from workspaces where created_by = $1`, ann).Scan(&wsID, &slug)
	}); err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, wsID, bob, "member")
	f = &fakePaddle{subs: map[string]string{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	cfg = map[string]string{"paddle_environment": "sandbox", "paddle_api_key": "pdl_key", "paddle_client_token": "test_tok",
		"paddle_webhook_secret": testSecret, "paddle_price_pro": "pri_pro", "paddle_price_ultimate": "pri_ult",
		"app_base_url": "http://app.test"}
	h := New(pool, sysconfig.New(pool), dbtest.Auth, workspace.New(pool, dbtest.Auth, nil, sysconfig.New(pool)).Member)
	h.paddle.base = srv.URL
	h.config = func(_ context.Context, key string) (string, error) { return cfg[key], nil }
	mux = http.NewServeMux()
	h.Register(mux)
	return
}

type state struct {
	plan             string
	comped           bool
	sub, status, cur *string
	cus              *string
	renews           *time.Time
	cancel           bool
}

func readState(t *testing.T, pool *pgxpool.Pool, userID string) state {
	t.Helper()
	var s state
	if err := pool.QueryRow(context.Background(), `select plan, plan_comped, paddle_subscription_id, subscription_status,
		billing_currency, paddle_customer_id, renews_at, cancel_at_period_end from users where id = $1`, userID).
		Scan(&s.plan, &s.comped, &s.sub, &s.status, &s.cur, &s.cus, &s.renews, &s.cancel); err != nil {
		t.Fatal(err)
	}
	return s
}

// TestWebhookSync: a signed subscription event sets the account's plan from the subscription Paddle returns (status,
// currency, renews_at, scheduled cancel, customer, an audit entry by the account in each workspace it owns), a bad
// signature is refused, a foreign checkout is ignored, a canceled subscription returns the account to Starter, a stale
// subscription is ignored, a comped account keeps its plan, and adjustments are only logged.
func TestWebhookSync(t *testing.T) {
	pool, mux, f, _, wsID, _, ann, bob := setup(t)
	ctx := context.Background()
	var ws2 string
	if err := pool.QueryRow(ctx, `insert into workspaces (name, slug, created_by) values ('Two', $1, $2) returning id::text`,
		dbtest.Name("two-"), ann).Scan(&ws2); err != nil {
		t.Fatal(err)
	}
	send := func(typ, data string, want int) {
		t.Helper()
		body := fmt.Sprintf(`{"event_id":"evt_%d","event_type":%q,"occurred_at":"2030-01-01T00:00:00Z","data":%s}`, time.Now().UnixNano(), typ, data)
		r := httptest.NewRequest("POST", "/api/billing/webhook", strings.NewReader(body))
		r.Header.Set("Paddle-Signature", sign(testSecret, time.Now().Unix(), []byte(body)))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s: %d %s, want %d", typ, w.Code, w.Body, want)
		}
	}
	read := func() state { t.Helper(); return readState(t, pool, ann) }
	sub1, sub2 := dbtest.Name("sub_"), dbtest.Name("sub_")

	checkout := func(user, txn string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `update users set paddle_checkout_id = $2 where id = $1`, user, txn); err != nil {
			t.Fatal(err)
		}
	}
	created := func(sub, txn string) string {
		return fmt.Sprintf(`{"id":%q,"transaction_id":%q,"custom_data":{"user_id":%q}}`, sub, txn, ann)
	}
	checkout(ann, "txn_a")
	checkout(bob, "txn_other")
	// Anyone can open a Paddle checkout naming this account; a subscription its checkout didn't make doesn't attach,
	// nor does another account's checkout.
	f.subs["sub_foreign"] = subJSON("sub_foreign", ann, "active", "pri_ult", false)
	send("subscription.created", created("sub_foreign", "txn_other"), 200)
	if s, b := read(), readState(t, pool, bob); s.plan != "starter" || s.sub != nil || b.plan != "starter" || b.sub != nil {
		t.Fatalf("foreign subscription attached: %+v %+v", s, b)
	}
	// Paddle's subscription has no custom_data here; the event's names the account, its transaction is our checkout.
	f.subs[sub1] = subJSON(sub1, "", "active", "pri_pro", false)
	send("subscription.created", created(sub1, "txn_a"), 200)
	s := read()
	if s.plan != "pro" || s.sub == nil || *s.sub != sub1 || *s.status != "active" || *s.cur != "inr" || s.cancel ||
		*s.cus != "ctm_1" || s.renews == nil || s.renews.Unix() != 1900000000 {
		t.Fatalf("after created: %+v", s)
	}
	for _, id := range []string{wsID, ws2} {
		var detail string
		var actor *string
		if err := pool.QueryRow(ctx, `select detail::text, actor_id::text from workspace_audit where workspace_id = $1 and action = 'billing.plan_changed'`, id).
			Scan(&detail, &actor); err != nil || actor == nil || *actor != ann || detail != `{"to": "pro", "from": "starter"}` {
			t.Fatalf("audit in %s: %s %v %v", id, detail, actor, err)
		}
	}

	r := httptest.NewRequest("POST", "/api/billing/webhook", strings.NewReader(`{"event_type":"subscription.canceled"}`))
	r.Header.Set("Paddle-Signature", sign("pdl_wrong", time.Now().Unix(), []byte(`{"event_type":"subscription.canceled"}`)))
	w := httptest.NewRecorder()
	if mux.ServeHTTP(w, r); w.Code != 400 {
		t.Fatalf("bad signature: %d", w.Code)
	}

	// Without custom_data the account is found by its subscription id.
	f.subs[sub1] = subJSON(sub1, "", "active", "pri_ult", true)
	send("subscription.updated", fmt.Sprintf(`{"id":%q}`, sub1), 200)
	if s := read(); s.plan != "ultimate" || !s.cancel {
		t.Fatalf("after scheduled cancel: %+v", s)
	}

	f.subs[sub1] = subJSON(sub1, ann, "canceled", "pri_ult", false)
	send("subscription.canceled", fmt.Sprintf(`{"id":%q}`, sub1), 200)
	if s := read(); s.plan != "starter" || s.sub != nil || s.status != nil || s.renews != nil || s.cancel || *s.cur != "inr" {
		t.Fatalf("after canceled: %+v", s)
	}

	checkout(ann, "txn_b")
	f.subs[sub2] = subJSON(sub2, ann, "active", "pri_pro", false)
	send("subscription.created", created(sub2, "txn_b"), 200)
	send("subscription.canceled", fmt.Sprintf(`{"id":%q}`, sub1), 200) // late event of the old subscription
	if s := read(); s.plan != "pro" || *s.sub != sub2 {
		t.Fatalf("stale subscription applied: %+v", s)
	}

	if _, err := pool.Exec(ctx, `update users set plan = 'ultimate', plan_comped = true where id = $1`, ann); err != nil {
		t.Fatal(err)
	}
	f.subs[sub2] = subJSON(sub2, ann, "past_due", "pri_pro", false)
	send("subscription.past_due", fmt.Sprintf(`{"id":%q}`, sub2), 200)
	if s := read(); s.plan != "ultimate" || !s.comped || *s.status != "past_due" {
		t.Fatalf("comped after past_due: %+v", s)
	}

	send("adjustment.created", fmt.Sprintf(`{"id":"adj_1","action":"refund","subscription_id":%q,"currency_code":"USD",
		"reason":"duplicate","totals":{"total":"1200"}}`, sub2), 200)
	send("transaction.completed", `{"id":"txn_1"}`, 200)
}

// TestCheckout: any account may start a checkout of its own, for a paid plan, while billing is on and it has no
// subscription; the transaction has the plan's price, the user in custom_data and its customer once known. Also the
// plan list, the account's billing state, change, the portal, and a workspace's view of its owner's plan.
func TestCheckout(t *testing.T) {
	pool, mux, f, cfg, wsID, slug, ann, bob := setup(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `insert into notification_settings (user_id, timezone) values ($1, 'Asia/Kolkata')
		on conflict (user_id) do update set timezone = excluded.timezone`, ann); err != nil {
		t.Fatal(err)
	}
	base := "/api/billing"
	do := func(user, method, path string, form url.Values, want int) map[string]any {
		t.Helper()
		code, body := dbtest.Do(t, mux, user, method, path, form)
		if code != want {
			t.Fatalf("%s %s %v: %d %s, want %d", method, path, form, code, body, want)
		}
		var v map[string]any
		_ = json.Unmarshal([]byte(body), &v)
		return v
	}
	pro, ult := url.Values{"plan": {"pro"}}, url.Values{"plan": {"ultimate"}}

	do("", "GET", base, nil, 401)
	do(ann, "POST", base+"/checkout", url.Values{"plan": {"starter"}}, 400)
	do(ann, "POST", base+"/portal", nil, 409)
	do(ann, "POST", base+"/change", ult, 409)
	cfg["paddle_client_token"] = ""
	do(ann, "POST", base+"/checkout", pro, 503)
	if b := do(ann, "GET", base, nil, 200); b["enabled"] != false {
		t.Fatalf("billing off: %v", b)
	}
	if p := do("", "GET", "/api/billing/plans", nil, 200)["plans"].([]any)[1].(map[string]any); fmt.Sprint(p["prices"]) != "map[inr:<nil> usd:<nil>]" {
		t.Fatalf("pro plan while off %v", p)
	}
	cfg["paddle_client_token"] = "test_tok"

	got := do(ann, "POST", base+"/checkout", pro, 200)
	if got["transaction_id"] != "txn_1" || got["client_token"] != "test_tok" || got["environment"] != "sandbox" ||
		!strings.HasSuffix(got["email"].(string), "@dbtest.test") {
		t.Fatalf("checkout %v", got)
	}
	var stored *string
	if err := pool.QueryRow(ctx, `select paddle_checkout_id from users where id = $1`, ann).Scan(&stored); err != nil || stored == nil || *stored != "txn_1" {
		t.Fatalf("checkout id stored %v %v", stored, err)
	}
	tx, _ := f.last("POST", "/transactions")
	if b, _ := json.Marshal(tx.body); string(b) != fmt.Sprintf(`{"custom_data":{"user_id":%q},"items":[{"price_id":"pri_pro","quantity":1}]}`, ann) {
		t.Fatalf("transaction %s", b)
	}
	if _, err := pool.Exec(ctx, `update users set paddle_customer_id = 'ctm_1' where id = $1`, ann); err != nil {
		t.Fatal(err)
	}
	do(ann, "POST", base+"/checkout", ult, 200)
	if tx, _ := f.last("POST", "/transactions"); tx.body["customer_id"] != "ctm_1" ||
		fmt.Sprint(tx.body["items"]) != "[map[price_id:pri_ult quantity:1]]" {
		t.Fatalf("transaction with a customer %v", tx.body)
	}
	do(bob, "POST", base+"/checkout", pro, 200) // Bob pays for his own account
	if tx, _ := f.last("POST", "/transactions"); fmt.Sprint(tx.body["custom_data"]) != "map[user_id:"+bob+"]" {
		t.Fatalf("Bob's transaction %v", tx.body)
	}

	plans := do("", "GET", "/api/billing/plans", nil, 200)["plans"].([]any)
	if p := plans[0].(map[string]any); fmt.Sprint(p["prices"]) != "map[inr:0 usd:0]" || p["workspaces"] != 1.0 || p["projects"] != 1.0 {
		t.Fatalf("starter plan %v", p)
	}
	if p := plans[1].(map[string]any); p["id"] != "pro" || p["members"] != 10.0 || p["workspaces"] != 2.0 || p["projects"] != 2.0 ||
		fmt.Sprint(p["prices"]) != "map[inr:69900 usd:1200]" {
		t.Fatalf("pro plan %v", p)
	}
	if p := plans[2].(map[string]any); p["members"] != nil || p["storage_bytes"] != nil || p["workspaces"] != nil ||
		p["projects"] != nil || fmt.Sprint(p["prices"]) != "map[inr:249900 usd:4900]" {
		t.Fatalf("ultimate plan %v", p)
	}
	do("", "GET", "/api/billing/plans", nil, 200)
	if _, n := f.last("GET", "/prices/pri_pro"); n != 1 {
		t.Fatalf("prices read %d times, want 1 (cached)", n)
	}

	b := do(bob, "GET", base, nil, 200)
	if b["plan"] != "starter" || b["currency"] != "usd" || b["enabled"] != true || fmt.Sprint(b["workspaces"]) != "map[limit:1 used:0]" {
		t.Fatalf("Bob's billing %v", b)
	}
	if b := do(bob, "GET", base+"?tz=Asia/Calcutta", nil, 200); b["currency"] != "inr" {
		t.Fatalf("Bob's billing with an Indian browser %v", b)
	}
	if b := do(ann, "GET", base, nil, 200); b["currency"] != "inr" || fmt.Sprint(b["workspaces"]) != "map[limit:1 used:1]" {
		t.Fatalf("Ann's billing %v", b)
	}

	wsBase := "/api/workspaces/" + slug + "/billing"
	if _, err := pool.Exec(ctx, `insert into projects (workspace_id, name, key) values ($1, 'Web', 'WEB')`, wsID); err != nil {
		t.Fatal(err)
	}
	v := do(bob, "GET", wsBase, nil, 200)
	if v["plan"] != "starter" || v["is_owner"] != false || fmt.Sprint(v["owner"]) != "map[id:"+ann+" name:Ann]" ||
		fmt.Sprint(v["projects"]) != "map[limit:1 used:1]" || fmt.Sprint(v["members"]) != "map[limit:5 used:2]" ||
		fmt.Sprint(v["storage"]) != "map[limit_bytes:2.147483648e+09 used_bytes:0]" {
		t.Fatalf("Bob's view of the workspace %v", v)
	}
	do(bob, "GET", "/api/workspaces/no-such-ws/billing", nil, 404)
	if _, err := pool.Exec(ctx, `update workspace_members set role_id = (select id from workspace_roles where workspace_id = $1 and builtin = 'guest')
		where workspace_id = $1 and user_id = $2`, wsID, bob); err != nil {
		t.Fatal(err)
	}
	do(bob, "GET", wsBase, nil, 403)

	sub := dbtest.Name("sub_")
	f.subs[sub] = subJSON(sub, ann, "active", "pri_pro", false)
	if _, err := pool.Exec(ctx, `update users set plan = 'pro', paddle_subscription_id = $2, subscription_status = 'active'
		where id = $1`, ann, sub); err != nil {
		t.Fatal(err)
	}
	if v := do(ann, "GET", wsBase, nil, 200); v["plan"] != "pro" || v["is_owner"] != true || fmt.Sprint(v["projects"]) != "map[limit:2 used:1]" {
		t.Fatalf("Ann's view of the workspace %v", v)
	}
	do(ann, "POST", base+"/checkout", pro, 409)
	do(ann, "POST", base+"/change", pro, 400)
	do(bob, "POST", base+"/change", ult, 409) // Bob's own account has no subscription
	if b := do(ann, "POST", base+"/change", ult, 200); b["plan"] != "ultimate" || b["status"] != "active" || b["currency"] != "inr" {
		t.Fatalf("change answered %v", b)
	}
	if patch, _ := f.last("PATCH", "/subscriptions/"+sub); patch.body["proration_billing_mode"] != "prorated_immediately" ||
		fmt.Sprint(patch.body["items"]) != "[map[price_id:pri_ult quantity:1]]" {
		t.Fatalf("change body %v", patch.body)
	}
	if s := readState(t, pool, ann); s.plan != "ultimate" {
		t.Fatalf("after change: %+v", s)
	}

	if got := do(ann, "POST", base+"/portal", nil, 200)["url"]; got != "https://customer-portal.paddle.test/1" {
		t.Fatalf("portal %v", got)
	}
	if ps, _ := f.last("POST", "/customers/ctm_1/portal-sessions"); fmt.Sprint(ps.body["subscription_ids"]) != "["+sub+"]" {
		t.Fatalf("portal body %v", ps.body)
	}
}
