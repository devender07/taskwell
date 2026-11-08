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
	"github.com/devender07/taskwell/backend/user/internal/secret"
	"github.com/devender07/taskwell/backend/user/internal/sysconfig"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

const (
	testSecret = "pdl_ntfset_test"
	testJWT    = "test-jwt-secret"
)

func sign(secret string, ts int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d:%s", ts, body)
	return fmt.Sprintf("ts=%d;h1=%s", ts, hex.EncodeToString(mac.Sum(nil)))
}

// checkoutSig is the sig checkout custom_data carries for the user, computed apart from the handler.
func checkoutSig(userID string) string {
	mac := hmac.New(sha256.New, secret.Key("taskwell checkout", "jwt_secret:"+testJWT))
	mac.Write([]byte(userID))
	return hex.EncodeToString(mac.Sum(nil))
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

// prices are the fake Paddle's prices: the catalog's shape, custom_data.taskwell_plan and billing_cycle.
var prices = map[string]string{
	"pri_starter_m": `{"id":"pri_starter_m","status":"active","custom_data":{"taskwell_plan":"starter"},"billing_cycle":{"interval":"month","frequency":1}}`,
	"pri_pro_m":     `{"id":"pri_pro_m","status":"active","custom_data":{"taskwell_plan":"pro"},"billing_cycle":{"interval":"month","frequency":1}}`,
	"pri_pro_y":     `{"id":"pri_pro_y","status":"active","custom_data":{"taskwell_plan":"pro"},"billing_cycle":{"interval":"year","frequency":1}}`,
	"pri_adv_y":     `{"id":"pri_adv_y","status":"active","custom_data":{"taskwell_plan":"advanced"},"billing_cycle":{"interval":"year","frequency":1}}`,
	"pri_nope":      `{"id":"pri_nope","status":"active","custom_data":null,"billing_cycle":{"interval":"month","frequency":1}}`,
	"pri_old":       `{"id":"pri_old","status":"archived","custom_data":{"taskwell_plan":"starter"},"billing_cycle":{"interval":"month","frequency":1}}`,
}

// sub is a subscription the fake Paddle holds; cd is its custom_data JSON ("" for null).
type sub struct {
	cd, status, price string
	cancel            bool
}

func (s sub) json(id string) string {
	sc, cd := "null", "null"
	if s.cancel {
		sc = `{"action":"cancel","effective_at":"2030-03-17T17:46:40Z"}`
	}
	if s.cd != "" {
		cd = s.cd
	}
	return fmt.Sprintf(`{"id":%q,"status":%q,"customer_id":"ctm_1","currency_code":"INR",
		"next_billed_at":"2030-03-17T17:46:40Z","scheduled_change":%s,"custom_data":%s,
		"items":[{"status":"active","quantity":1,"price":%s}]}`, id, s.status, sc, cd, prices[s.price])
}

// fakePaddle answers the Paddle calls billing makes and records each request.
type fakePaddle struct {
	mu   sync.Mutex
	subs map[string]sub
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
	id, isSub := strings.CutPrefix(r.URL.Path, "/subscriptions/")
	s, known := f.subs[id]
	switch p := r.URL.Path; {
	case strings.HasPrefix(p, "/prices/") && prices[strings.TrimPrefix(p, "/prices/")] != "":
		reply(prices[strings.TrimPrefix(p, "/prices/")])
	case strings.HasPrefix(p, "/customers/") && strings.HasSuffix(p, "/portal-sessions"):
		reply(`{"id":"cpls_1","urls":{"general":{"overview":"https://customer-portal.paddle.test/1"},"subscriptions":[]}}`)
	case isSub && known:
		if r.Method == http.MethodPatch {
			s.price = body["items"].([]any)[0].(map[string]any)["price_id"].(string)
			f.subs[id] = s
		}
		reply(s.json(id))
	default:
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":{"type":"request_error","code":"not_found","detail":"no such entity"}}`)
	}
}

func (f *fakePaddle) set(id string, s sub) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subs[id] = s
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

// signedCD is checkout custom_data for the user with sig.
func signedCD(userID, sig string) string {
	return fmt.Sprintf(`{"user_id":%q,"sig":%q}`, userID, sig)
}

// setup makes Ann's workspace with Bob as a plain member, and a billing handler against a fake Paddle whose config
// is cfg (every paddle_ key set).
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
	f = &fakePaddle{subs: map[string]sub{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	cfg = map[string]string{"paddle_environment": "sandbox", "paddle_api_key": "pdl_key", "paddle_webhook_secret": testSecret}
	h := New(pool, sysconfig.New(pool), testJWT, dbtest.Auth, workspace.New(pool, dbtest.Auth, nil, sysconfig.New(pool)).Member)
	h.paddle.base = srv.URL
	h.config = func(_ context.Context, key string) (string, error) { return cfg[key], nil }
	mux = http.NewServeMux()
	h.Register(mux)
	return
}

type state struct {
	plan                     string
	comped                   bool
	sub, status, cur, period *string
	cus                      *string
	renews                   *time.Time
	cancel                   bool
}

func readState(t *testing.T, pool *pgxpool.Pool, userID string) state {
	t.Helper()
	var s state
	if err := pool.QueryRow(context.Background(), `select plan, plan_comped, paddle_subscription_id, subscription_status,
		billing_currency, billing_interval, paddle_customer_id, renews_at, cancel_at_period_end from users where id = $1`, userID).
		Scan(&s.plan, &s.comped, &s.sub, &s.status, &s.cur, &s.period, &s.cus, &s.renews, &s.cancel); err != nil {
		t.Fatal(err)
	}
	return s
}

// TestWebhookSync: a subscription links to the account its custom_data names only when the sig verifies and that
// account has no other subscription in force; its plan is its price's custom_data.taskwell_plan (unknown: a 500 so
// Paddle retries) with the price's interval; trialing keeps the plan; canceled returns the account to Free; a stale
// subscription is ignored; a comped account keeps its plan; a bad webhook signature is refused; adjustments are only
// logged.
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
			t.Fatalf("%s %s: %d %s, want %d", typ, data, w.Code, w.Body, want)
		}
	}
	event := func(id, cd string) string {
		if cd == "" {
			return fmt.Sprintf(`{"id":%q}`, id)
		}
		return fmt.Sprintf(`{"id":%q,"custom_data":%s}`, id, cd)
	}
	read := func() state { t.Helper(); return readState(t, pool, ann) }
	sub1, sub2, sub3 := dbtest.Name("sub_"), dbtest.Name("sub_"), dbtest.Name("sub_")
	good := signedCD(ann, checkoutSig(ann))

	// Paddle.js checkout takes any custom_data: a forged, another user's or missing sig doesn't link.
	for _, cd := range []string{signedCD(ann, "00ff"), signedCD(ann, checkoutSig(bob)), fmt.Sprintf(`{"user_id":%q}`, ann), ""} {
		id := dbtest.Name("sub_forged_")
		f.set(id, sub{cd: cd, status: "active", price: "pri_adv_y"})
		send("subscription.created", event(id, ""), 200)
		if s := read(); s.plan != "free" || s.sub != nil {
			t.Fatalf("custom_data %s linked: %+v", cd, s)
		}
	}

	// Paddle's subscription has no custom_data here; the event's is signed.
	f.set(sub1, sub{status: "active", price: "pri_pro_m"})
	send("subscription.created", event(sub1, good), 200)
	s := read()
	if s.plan != "pro" || s.sub == nil || *s.sub != sub1 || *s.status != "active" || *s.cur != "inr" || *s.period != "month" ||
		s.cancel || *s.cus != "ctm_1" || s.renews == nil || s.renews.Unix() != 1900000000 {
		t.Fatalf("after created: %+v", s)
	}
	for _, id := range []string{wsID, ws2} {
		var detail string
		var actor *string
		if err := pool.QueryRow(ctx, `select detail::text, actor_id::text from workspace_audit where workspace_id = $1 and action = 'billing.plan_changed'`, id).
			Scan(&detail, &actor); err != nil || actor == nil || *actor != ann || detail != `{"to": "pro", "from": "free"}` {
			t.Fatalf("audit in %s: %s %v %v", id, detail, actor, err)
		}
	}

	// A second signed subscription doesn't replace the one in force.
	f.set(sub3, sub{cd: good, status: "active", price: "pri_starter_m"})
	send("subscription.created", event(sub3, good), 200)
	if s := read(); s.plan != "pro" || *s.sub != sub1 {
		t.Fatalf("re-linked: %+v", s)
	}

	r := httptest.NewRequest("POST", "/api/billing/webhook", strings.NewReader(`{"event_type":"subscription.canceled"}`))
	r.Header.Set("Paddle-Signature", sign("pdl_wrong", time.Now().Unix(), []byte(`{"event_type":"subscription.canceled"}`)))
	w := httptest.NewRecorder()
	if mux.ServeHTTP(w, r); w.Code != 400 {
		t.Fatalf("bad signature: %d", w.Code)
	}

	// Found by its subscription id, unsigned; the plan and interval follow the price.
	f.set(sub1, sub{status: "active", price: "pri_adv_y", cancel: true})
	send("subscription.updated", event(sub1, ""), 200)
	if s := read(); s.plan != "advanced" || *s.period != "year" || !s.cancel {
		t.Fatalf("after scheduled cancel: %+v", s)
	}
	f.set(sub1, sub{status: "trialing", price: "pri_adv_y"})
	send("subscription.updated", event(sub1, ""), 200)
	if s := read(); s.plan != "advanced" || *s.status != "trialing" || s.cancel {
		t.Fatalf("trialing: %+v", s)
	}
	f.set(sub1, sub{status: "active", price: "pri_nope"})
	send("subscription.updated", event(sub1, ""), 500)
	if s := read(); s.plan != "advanced" {
		t.Fatalf("price without taskwell_plan applied: %+v", s)
	}

	f.set(sub1, sub{cd: good, status: "canceled", price: "pri_adv_y"})
	send("subscription.canceled", event(sub1, ""), 200)
	if s := read(); s.plan != "free" || s.sub != nil || s.status != nil || s.renews != nil || s.cancel || s.cur != nil || s.period != nil {
		t.Fatalf("after canceled: %+v", s)
	}

	f.set(sub2, sub{cd: good, status: "active", price: "pri_pro_y"})
	send("subscription.created", event(sub2, ""), 200)
	send("subscription.canceled", event(sub1, ""), 200) // late event of the old subscription
	if s := read(); s.plan != "pro" || *s.sub != sub2 || *s.period != "year" {
		t.Fatalf("stale subscription applied: %+v", s)
	}

	if _, err := pool.Exec(ctx, `update users set plan = 'advanced', plan_comped = true where id = $1`, ann); err != nil {
		t.Fatal(err)
	}
	f.set(sub2, sub{cd: good, status: "past_due", price: "pri_pro_y"})
	send("subscription.past_due", event(sub2, ""), 200)
	if s := read(); s.plan != "advanced" || !s.comped || *s.status != "past_due" {
		t.Fatalf("comped after past_due: %+v", s)
	}

	send("adjustment.created", fmt.Sprintf(`{"id":"adj_1","action":"refund","subscription_id":%q,"currency_code":"USD",
		"reason":"duplicate","totals":{"total":"1200"}}`, sub2), 200)
	send("transaction.completed", `{"id":"txn_1"}`, 200)
}

// TestCheckout: checkout answers the account's signed custom_data and email (no Paddle call) while billing is on and
// it has no subscription and isn't comped; also the account's billing state, change (by a price whose custom_data
// names a paid plan, not the current one), the portal, and a workspace's view of its owner's plan.
func TestCheckout(t *testing.T) {
	pool, mux, f, cfg, wsID, slug, ann, bob := setup(t)
	ctx := context.Background()
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
	price := func(id string) url.Values { return url.Values{"price_id": {id}} }

	do("", "GET", base, nil, 401)
	do(ann, "POST", base+"/checkout", nil, 400)
	do(ann, "POST", base+"/portal", nil, 409)
	do(ann, "POST", base+"/change", price("pri_pro_y"), 409)
	for _, env := range []string{"", "live", "Sandbox"} {
		cfg["paddle_environment"] = env
		do(ann, "POST", base+"/checkout", price("pri_pro_m"), 503)
		if b := do(ann, "GET", base, nil, 200); b["enabled"] != false {
			t.Fatalf("billing with environment %q: %v", env, b)
		}
	}
	cfg["paddle_environment"], cfg["paddle_api_key"] = "production", ""
	do(ann, "POST", base+"/checkout", price("pri_pro_m"), 503)
	cfg["paddle_environment"], cfg["paddle_api_key"] = "sandbox", "pdl_key"

	got := do(ann, "POST", base+"/checkout", price("pri_pro_m"), 200)
	if cd := got["custom_data"].(map[string]any); cd["user_id"] != ann || cd["sig"] != checkoutSig(ann) ||
		!strings.HasSuffix(got["email"].(string), "@dbtest.test") || len(got) != 2 {
		t.Fatalf("checkout %v", got)
	}
	if got := do(bob, "POST", base+"/checkout", price("pri_pro_m"), 200); got["custom_data"].(map[string]any)["sig"] != checkoutSig(bob) {
		t.Fatalf("Bob's checkout %v", got) // Bob pays for his own account
	}
	if f.mu.Lock(); len(f.reqs) != 0 {
		t.Fatalf("checkout called Paddle: %v", f.reqs)
	}
	f.mu.Unlock()
	do("", "GET", base+"/plans", nil, 404)

	if b := do(bob, "GET", base, nil, 200); b["plan"] != "free" || b["currency"] != nil || b["interval"] != nil || b["enabled"] != true ||
		fmt.Sprint(b["workspaces"]) != "map[limit:1 used:0]" {
		t.Fatalf("Bob's billing %v", b)
	}

	wsBase := "/api/workspaces/" + slug + "/billing"
	if _, err := pool.Exec(ctx, `insert into projects (workspace_id, name, key) values ($1, 'Web', 'WEB')`, wsID); err != nil {
		t.Fatal(err)
	}
	v := do(bob, "GET", wsBase, nil, 200)
	if v["plan"] != "free" || v["is_owner"] != false || fmt.Sprint(v["owner"]) != "map[id:"+ann+" name:Ann]" ||
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

	id := dbtest.Name("sub_")
	f.set(id, sub{cd: signedCD(ann, checkoutSig(ann)), status: "active", price: "pri_pro_m"})
	if _, err := pool.Exec(ctx, `update users set plan = 'pro', paddle_subscription_id = $2, subscription_status = 'active',
		paddle_customer_id = 'ctm_1', billing_currency = 'inr', billing_interval = 'month' where id = $1`, ann, id); err != nil {
		t.Fatal(err)
	}
	if v := do(ann, "GET", wsBase, nil, 200); v["plan"] != "pro" || v["is_owner"] != true || fmt.Sprint(v["projects"]) != "map[limit:<nil> used:1]" {
		t.Fatalf("Ann's view of the workspace %v", v)
	}
	if b := do(ann, "GET", base, nil, 200); b["currency"] != "inr" || b["interval"] != "month" || fmt.Sprint(b["workspaces"]) != "map[limit:3 used:1]" {
		t.Fatalf("Ann's billing %v", b)
	}
	do(ann, "POST", base+"/checkout", price("pri_adv_y"), 409)
	dbtest.SetPlan(t, pool, bob, "pro")
	if _, err := pool.Exec(ctx, `update users set plan_comped = true where id = $1`, bob); err != nil {
		t.Fatal(err)
	}
	do(bob, "POST", base+"/checkout", price("pri_pro_m"), 409) // comped
	do(bob, "POST", base+"/change", price("pri_adv_y"), 409)   // Bob's own account has no subscription

	for _, p := range []string{"", "pri_missing", "pri_nope", "pri_old", "pri_pro_m"} {
		do(ann, "POST", base+"/change", price(p), 400)
	}
	if _, n := f.last("PATCH", "/subscriptions/"+id); n != 0 {
		t.Fatalf("a refused change patched the subscription")
	}
	if b := do(ann, "POST", base+"/change", price("pri_pro_y"), 200); b["plan"] != "pro" || b["interval"] != "year" ||
		b["status"] != "active" || b["currency"] != "inr" {
		t.Fatalf("interval change answered %v", b)
	}
	if patch, _ := f.last("PATCH", "/subscriptions/"+id); patch.body["proration_billing_mode"] != "prorated_immediately" ||
		fmt.Sprint(patch.body["items"]) != "[map[price_id:pri_pro_y quantity:1]]" {
		t.Fatalf("change body %v", patch.body)
	}
	if b := do(ann, "POST", base+"/change", price("pri_starter_m"), 200); b["plan"] != "starter" || b["interval"] != "month" {
		t.Fatalf("plan change answered %v", b)
	}
	if s := readState(t, pool, ann); s.plan != "starter" || *s.period != "month" {
		t.Fatalf("after change: %+v", s)
	}

	if got := do(ann, "POST", base+"/portal", nil, 200)["url"]; got != "https://customer-portal.paddle.test/1" {
		t.Fatalf("portal %v", got)
	}
	if ps, _ := f.last("POST", "/customers/ctm_1/portal-sessions"); fmt.Sprint(ps.body["subscription_ids"]) != "["+id+"]" {
		t.Fatalf("portal body %v", ps.body)
	}
}
