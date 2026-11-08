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
		reply(`{"id":"pri_pro","unit_price":{"amount":"2900","currency_code":"USD"},
			"unit_price_overrides":[{"country_codes":["IN"],"unit_price":{"amount":"149900","currency_code":"INR"}}]}`)
	case p == "/prices/pri_ult":
		reply(`{"id":"pri_ult","unit_price":{"amount":"7900","currency_code":"USD"},
			"unit_price_overrides":[{"country_codes":["GB","IN"],"unit_price":{"amount":"399900","currency_code":"INR"}}]}`)
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

func subJSON(id, workspaceID, status, priceID string, cancel bool) string {
	sc := "null"
	if cancel {
		sc = `{"action":"cancel","effective_at":"2030-03-17T17:46:40Z"}`
	}
	cd := "null"
	if workspaceID != "" {
		cd = fmt.Sprintf(`{"workspace_id":%q}`, workspaceID)
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

func readState(t *testing.T, pool *pgxpool.Pool, wsID string) state {
	t.Helper()
	var s state
	if err := pool.QueryRow(context.Background(), `select plan, plan_comped, paddle_subscription_id, subscription_status,
		billing_currency, paddle_customer_id, renews_at, cancel_at_period_end from workspaces where id = $1`, wsID).
		Scan(&s.plan, &s.comped, &s.sub, &s.status, &s.cur, &s.cus, &s.renews, &s.cancel); err != nil {
		t.Fatal(err)
	}
	return s
}

// TestWebhookSync: a signed subscription event sets the plan from the subscription Paddle returns (status, currency,
// renews_at, scheduled cancel, customer, an audit entry with no actor), a bad signature is refused, a canceled
// subscription returns the workspace to Starter, a stale subscription is ignored, a comped workspace keeps its plan,
// and a refund and a chargeback are audited.
func TestWebhookSync(t *testing.T) {
	pool, mux, f, _, wsID, _, _, _ := setup(t)
	ctx := context.Background()
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
	read := func() state { t.Helper(); return readState(t, pool, wsID) }
	sub1, sub2 := dbtest.Name("sub_"), dbtest.Name("sub_")

	checkout := func(txn string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `update workspaces set paddle_checkout_id = $2 where id = $1`, wsID, txn); err != nil {
			t.Fatal(err)
		}
	}
	created := func(sub, txn string) string {
		return fmt.Sprintf(`{"id":%q,"transaction_id":%q,"custom_data":{"workspace_id":%q}}`, sub, txn, wsID)
	}
	checkout("txn_a")
	// Anyone can open a Paddle checkout naming this workspace; a subscription its checkout didn't make doesn't attach.
	f.subs["sub_foreign"] = subJSON("sub_foreign", wsID, "active", "pri_ult", false)
	send("subscription.created", created("sub_foreign", "txn_other"), 200)
	if s := read(); s.plan != "starter" || s.sub != nil {
		t.Fatalf("foreign subscription attached: %+v", s)
	}
	// Paddle's subscription has no custom_data here; the event's names the workspace, its transaction is our checkout.
	f.subs[sub1] = subJSON(sub1, "", "active", "pri_pro", false)
	send("subscription.created", created(sub1, "txn_a"), 200)
	s := read()
	if s.plan != "pro" || s.sub == nil || *s.sub != sub1 || *s.status != "active" || *s.cur != "inr" || s.cancel ||
		*s.cus != "ctm_1" || s.renews == nil || s.renews.Unix() != 1900000000 {
		t.Fatalf("after created: %+v", s)
	}
	var detail string
	var actor *string
	if err := pool.QueryRow(ctx, `select detail::text, actor_id::text from workspace_audit where workspace_id = $1 and action = 'billing.plan_changed'`, wsID).
		Scan(&detail, &actor); err != nil || actor != nil || detail != `{"to": "pro", "from": "starter"}` {
		t.Fatalf("audit %s %v %v", detail, actor, err)
	}

	r := httptest.NewRequest("POST", "/api/billing/webhook", strings.NewReader(`{"event_type":"subscription.canceled"}`))
	r.Header.Set("Paddle-Signature", sign("pdl_wrong", time.Now().Unix(), []byte(`{"event_type":"subscription.canceled"}`)))
	w := httptest.NewRecorder()
	if mux.ServeHTTP(w, r); w.Code != 400 {
		t.Fatalf("bad signature: %d", w.Code)
	}

	// Without custom_data the workspace is found by its subscription id.
	f.subs[sub1] = subJSON(sub1, "", "active", "pri_ult", true)
	send("subscription.updated", fmt.Sprintf(`{"id":%q}`, sub1), 200)
	if s := read(); s.plan != "ultimate" || !s.cancel {
		t.Fatalf("after scheduled cancel: %+v", s)
	}

	f.subs[sub1] = subJSON(sub1, wsID, "canceled", "pri_ult", false)
	send("subscription.canceled", fmt.Sprintf(`{"id":%q}`, sub1), 200)
	if s := read(); s.plan != "starter" || s.sub != nil || s.status != nil || s.renews != nil || s.cancel || *s.cur != "inr" {
		t.Fatalf("after canceled: %+v", s)
	}

	checkout("txn_b")
	f.subs[sub2] = subJSON(sub2, wsID, "active", "pri_pro", false)
	send("subscription.created", created(sub2, "txn_b"), 200)
	send("subscription.canceled", fmt.Sprintf(`{"id":%q}`, sub1), 200) // late event of the old subscription
	if s := read(); s.plan != "pro" || *s.sub != sub2 {
		t.Fatalf("stale subscription applied: %+v", s)
	}

	if _, err := pool.Exec(ctx, `update workspaces set plan = 'ultimate', plan_comped = true where id = $1`, wsID); err != nil {
		t.Fatal(err)
	}
	f.subs[sub2] = subJSON(sub2, wsID, "past_due", "pri_pro", false)
	send("subscription.past_due", fmt.Sprintf(`{"id":%q}`, sub2), 200)
	if s := read(); s.plan != "ultimate" || !s.comped || *s.status != "past_due" {
		t.Fatalf("comped after past_due: %+v", s)
	}

	adj := func(action string) string {
		return fmt.Sprintf(`{"id":"adj_1","action":%q,"subscription_id":%q,"currency_code":"USD","reason":"duplicate",
			"totals":{"total":"2900"}}`, action, sub2)
	}
	send("adjustment.created", adj("refund"), 200)
	send("adjustment.created", adj("chargeback"), 200)
	send("adjustment.created", adj("credit"), 200)
	send("transaction.completed", `{"id":"txn_1"}`, 200)
	for action, want := range map[string]int{"billing.refunded": 1, "billing.disputed": 1} {
		var n int
		if err := pool.QueryRow(ctx, `select count(*) from workspace_audit where workspace_id = $1 and action = $2
			and detail->>'amount' = '2900'`, wsID, action).Scan(&n); err != nil || n != want {
			t.Fatalf("%s audited %d %v", action, n, err)
		}
	}
}

// TestCheckout: only billing.manage may start a checkout, for a paid plan, while billing is on and there's no
// subscription; the transaction has the plan's price, the workspace in custom_data and its customer once known. Also
// the plan list's prices, the billing state, change and the portal.
func TestCheckout(t *testing.T) {
	pool, mux, f, cfg, wsID, slug, ann, bob := setup(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `insert into notification_settings (user_id, timezone) values ($1, 'Asia/Kolkata')
		on conflict (user_id) do update set timezone = excluded.timezone`, ann); err != nil {
		t.Fatal(err)
	}
	base := "/api/workspaces/" + slug + "/billing"
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

	do(bob, "POST", base+"/checkout", pro, 403)
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
	if err := pool.QueryRow(ctx, `select paddle_checkout_id from workspaces where id = $1`, wsID).Scan(&stored); err != nil || stored == nil || *stored != "txn_1" {
		t.Fatalf("checkout id stored %v %v", stored, err)
	}
	tx, _ := f.last("POST", "/transactions")
	if b, _ := json.Marshal(tx.body); string(b) != fmt.Sprintf(`{"custom_data":{"workspace_id":%q},"items":[{"price_id":"pri_pro","quantity":1}]}`, wsID) {
		t.Fatalf("transaction %s", b)
	}
	if _, err := pool.Exec(ctx, `update workspaces set paddle_customer_id = 'ctm_1' where id = $1`, wsID); err != nil {
		t.Fatal(err)
	}
	do(ann, "POST", base+"/checkout", ult, 200)
	if tx, _ := f.last("POST", "/transactions"); tx.body["customer_id"] != "ctm_1" ||
		fmt.Sprint(tx.body["items"]) != "[map[price_id:pri_ult quantity:1]]" {
		t.Fatalf("transaction with a customer %v", tx.body)
	}

	plans := do("", "GET", "/api/billing/plans", nil, 200)["plans"].([]any)
	if p := plans[0].(map[string]any); fmt.Sprint(p["prices"]) != "map[inr:0 usd:0]" {
		t.Fatalf("starter plan %v", p)
	}
	if p := plans[1].(map[string]any); p["id"] != "pro" || p["members"] != 10.0 || fmt.Sprint(p["prices"]) != "map[inr:149900 usd:2900]" {
		t.Fatalf("pro plan %v", p)
	}
	if p := plans[2].(map[string]any); p["members"] != nil || p["storage_bytes"] != nil || fmt.Sprint(p["prices"]) != "map[inr:399900 usd:7900]" {
		t.Fatalf("ultimate plan %v", p)
	}
	do("", "GET", "/api/billing/plans", nil, 200)
	if _, n := f.last("GET", "/prices/pri_pro"); n != 1 {
		t.Fatalf("prices read %d times, want 1 (cached)", n)
	}

	b := do(bob, "GET", base, nil, 200)
	if b["plan"] != "starter" || b["can_manage"] != false || b["currency"] != "usd" || b["enabled"] != true ||
		fmt.Sprint(b["members"]) != "map[limit:5 used:2]" {
		t.Fatalf("Bob's billing %v", b)
	}
	if b := do(bob, "GET", base+"?tz=Asia/Calcutta", nil, 200); b["currency"] != "inr" {
		t.Fatalf("Bob's billing with an Indian browser %v", b)
	}
	if b := do(ann, "GET", base, nil, 200); b["can_manage"] != true || b["currency"] != "inr" {
		t.Fatalf("Ann's billing %v", b)
	}

	sub := dbtest.Name("sub_")
	f.subs[sub] = subJSON(sub, wsID, "active", "pri_pro", false)
	if _, err := pool.Exec(ctx, `update workspaces set plan = 'pro', paddle_subscription_id = $2, subscription_status = 'active'
		where id = $1`, wsID, sub); err != nil {
		t.Fatal(err)
	}
	do(ann, "POST", base+"/checkout", pro, 409)
	do(ann, "POST", base+"/change", pro, 400)
	do(bob, "POST", base+"/change", ult, 403)
	if b := do(ann, "POST", base+"/change", ult, 200); b["plan"] != "ultimate" || b["status"] != "active" || b["currency"] != "inr" {
		t.Fatalf("change answered %v", b)
	}
	if patch, _ := f.last("PATCH", "/subscriptions/"+sub); patch.body["proration_billing_mode"] != "prorated_immediately" ||
		fmt.Sprint(patch.body["items"]) != "[map[price_id:pri_ult quantity:1]]" {
		t.Fatalf("change body %v", patch.body)
	}
	if s := readState(t, pool, wsID); s.plan != "ultimate" {
		t.Fatalf("after change: %+v", s)
	}

	if got := do(ann, "POST", base+"/portal", nil, 200)["url"]; got != "https://customer-portal.paddle.test/1" {
		t.Fatalf("portal %v", got)
	}
	if ps, _ := f.last("POST", "/customers/ctm_1/portal-sessions"); fmt.Sprint(ps.body["subscription_ids"]) != "["+sub+"]" {
		t.Fatalf("portal body %v", ps.body)
	}
}
