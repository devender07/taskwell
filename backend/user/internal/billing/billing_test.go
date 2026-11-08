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

func TestGrantsAccess(t *testing.T) {
	for status, want := range map[string]bool{
		"active": true, "trialing": true, "past_due": true, "paused": false, "canceled": false, "": false, "Active": false,
	} {
		if got := grantsAccess(status); got != want {
			t.Errorf("%q: got %v, want %v", status, got, want)
		}
	}
}

// prices are the catalog's shape: product_id, custom_data.taskwell_plan and billing_cycle.
var prices = map[string]string{
	"pri_starter_m": `{"id":"pri_starter_m","product_id":"pro_starter","status":"active","custom_data":{"taskwell_plan":"starter"},"billing_cycle":{"interval":"month","frequency":1}}`,
	"pri_pro_m":     `{"id":"pri_pro_m","product_id":"pro_pro","status":"active","custom_data":{"taskwell_plan":"pro"},"billing_cycle":{"interval":"month","frequency":1}}`,
	"pri_pro_y":     `{"id":"pri_pro_y","product_id":"pro_pro","status":"active","custom_data":{"taskwell_plan":"pro"},"billing_cycle":{"interval":"year","frequency":1}}`,
	"pri_adv_y":     `{"id":"pri_adv_y","product_id":"pro_adv","status":"active","custom_data":{"taskwell_plan":"advanced"},"billing_cycle":{"interval":"year","frequency":1}}`,
	"pri_nope":      `{"id":"pri_nope","product_id":"pro_x","status":"active","custom_data":null,"billing_cycle":{"interval":"month","frequency":1}}`,
	"pri_old":       `{"id":"pri_old","product_id":"pro_starter","status":"archived","custom_data":{"taskwell_plan":"starter"},"billing_cycle":{"interval":"month","frequency":1}}`,
}

// sub is a Paddle subscription entity; cd is its custom_data JSON ("" for null).
type sub struct {
	id, customer, cd, status, price string
	updated                         time.Time
	cancel                          bool
}

func (s sub) json() string {
	sc, cd := "null", "null"
	if s.cancel {
		sc = `{"action":"cancel","effective_at":"2030-03-17T17:46:40Z","resume_at":null}`
	}
	if s.cd != "" {
		cd = s.cd
	}
	return fmt.Sprintf(`{"id":%q,"status":%q,"customer_id":%q,"address_id":"add_1","currency_code":"INR",
		"created_at":"2026-01-01T00:00:00Z","updated_at":%q,"next_billed_at":"2030-03-17T17:46:40Z",
		"billing_cycle":{"interval":"month","frequency":1},"scheduled_change":%s,"custom_data":%s,
		"items":[{"status":"active","quantity":1,"price":%s}]}`,
		s.id, s.status, s.customer, s.updated.Format(time.RFC3339Nano), sc, cd, prices[s.price])
}

// fakePaddle answers the Paddle calls billing makes (price reads, subscription patches, portal sessions) and records
// each request.
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
	case strings.HasSuffix(p, "/activate") && r.Method == http.MethodPost:
		id := strings.TrimSuffix(id, "/activate")
		s, ok := f.subs[id]
		if !ok || s.status != "trialing" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":{"type":"request_error","code":"subscription_not_trialing","detail":"not trialing"}}`)
			return
		}
		s.status, s.updated = "active", s.updated.Add(time.Minute)
		f.subs[id] = s
		reply(s.json())
	case isSub && known && r.Method == http.MethodPatch:
		s.price = body["items"].([]any)[0].(map[string]any)["price_id"].(string)
		s.updated = time.Now()
		f.subs[id] = s
		reply(s.json())
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

// signedCD is checkout custom_data for the user with sig.
func signedCD(userID, sig string) string {
	return fmt.Sprintf(`{"user_id":%q,"sig":%q}`, userID, sig)
}

type fixture struct {
	pool                 *pgxpool.Pool
	mux                  *http.ServeMux
	f                    *fakePaddle
	cfg                  map[string]string
	wsID, slug, ann, bob string
	tag                  string // in every Paddle id the test makes
}

// setup makes Ann's workspace with Bob as a plain member, and a billing handler against a fake Paddle whose config
// is cfg (every paddle_ key set).
func setup(t *testing.T) *fixture {
	t.Helper()
	x := &fixture{pool: dbtest.Pool(t), tag: dbtest.Name("")}
	ctx := context.Background()
	x.ann, x.bob = dbtest.User(t, x.pool, "Ann"), dbtest.User(t, x.pool, "Bob")
	t.Cleanup(func() {
		_, _ = x.pool.Exec(context.Background(), `delete from paddle_subscriptions where subscription_id like '%'||$1||'%'`, x.tag)
		_, _ = x.pool.Exec(context.Background(), `delete from paddle_customers where customer_id like '%'||$1||'%'`, x.tag)
	})
	if err := pgx.BeginFunc(ctx, x.pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, x.ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `select id::text, slug from workspaces where created_by = $1`, x.ann).Scan(&x.wsID, &x.slug)
	}); err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, x.pool, x.wsID, x.bob, "member")
	x.f = &fakePaddle{subs: map[string]sub{}}
	srv := httptest.NewServer(x.f)
	t.Cleanup(srv.Close)
	x.cfg = map[string]string{"paddle_environment": "sandbox", "paddle_api_key": "pdl_key", "paddle_webhook_secret": testSecret}
	h := New(x.pool, sysconfig.New(x.pool), testJWT, dbtest.Auth, workspace.New(x.pool, dbtest.Auth, nil, sysconfig.New(x.pool)).Member)
	h.paddle.base = srv.URL
	h.config = func(_ context.Context, key string) (string, error) { return x.cfg[key], nil }
	x.mux = http.NewServeMux()
	h.Register(x.mux)
	return x
}

// send delivers a signed webhook event and checks the status.
func (x *fixture) send(t *testing.T, typ, data string, want int) {
	t.Helper()
	body := fmt.Sprintf(`{"event_id":"evt_%d","event_type":%q,"occurred_at":"2030-01-01T00:00:00Z","notification_id":"ntf_1","data":%s}`,
		time.Now().UnixNano(), typ, data)
	r := httptest.NewRequest("POST", "/api/billing/webhook", strings.NewReader(body))
	r.Header.Set("Paddle-Signature", sign(testSecret, time.Now().Unix(), []byte(body)))
	w := httptest.NewRecorder()
	x.mux.ServeHTTP(w, r)
	if w.Code != want {
		t.Fatalf("%s %s: %d %s, want %d", typ, data, w.Code, w.Body, want)
	}
}

func (x *fixture) do(t *testing.T, user, method, path string, form url.Values, want int) map[string]any {
	t.Helper()
	code, body := dbtest.Do(t, x.mux, user, method, path, form)
	if code != want {
		t.Fatalf("%s %s %v: %d %s, want %d", method, path, form, code, body, want)
	}
	var v map[string]any
	_ = json.Unmarshal([]byte(body), &v)
	return v
}

// row is a mirrored subscription.
type row struct {
	user                          *string
	status, price, product, plan  string
	interval, currency, scheduled *string
	updated                       time.Time
}

func (x *fixture) row(t *testing.T, id string) row {
	t.Helper()
	var r row
	if err := x.pool.QueryRow(context.Background(), `select user_id::text, status, price_id, product_id, plan, billing_interval,
		currency_code, scheduled_change_action, paddle_updated_at from paddle_subscriptions where subscription_id = $1`, id).
		Scan(&r.user, &r.status, &r.price, &r.product, &r.plan, &r.interval, &r.currency, &r.scheduled, &r.updated); err != nil {
		t.Fatalf("subscription %s: %v", id, err)
	}
	return r
}

func (x *fixture) plan(t *testing.T, userID string) string {
	t.Helper()
	var p string
	if err := x.pool.QueryRow(context.Background(), `select plan from users where id = $1`, userID).Scan(&p); err != nil {
		t.Fatal(err)
	}
	return p
}

func (x *fixture) customer(t *testing.T, id string) (user *string, email string) {
	t.Helper()
	if err := x.pool.QueryRow(context.Background(), `select user_id::text, email from paddle_customers where customer_id = $1`, id).
		Scan(&user, &email); err != nil {
		t.Fatalf("customer %s: %v", id, err)
	}
	return user, email
}

func (x *fixture) audits(t *testing.T, wsID string) int {
	t.Helper()
	var n int
	if err := x.pool.QueryRow(context.Background(), `select count(*) from workspace_audit where workspace_id = $1
		and action = 'billing.plan_changed'`, wsID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func is(p *string, want string) bool { return p != nil && *p == want }

// TestWebhook: subscription and customer events are mirrored from their payload, order-safe and idempotent; a
// subscription is linked to an account only by signed custom_data while that account has no other subscription
// granting access; the account's plan follows its subscription granting access (active, trialing, past_due), else
// Free, unless comped.
func TestWebhook(t *testing.T) {
	x := setup(t)
	ctx := context.Background()
	var ws2 string
	if err := x.pool.QueryRow(ctx, `insert into workspaces (name, slug, created_by) values ('Two', $1, $2) returning id::text`,
		dbtest.Name("two-"), x.ann).Scan(&ws2); err != nil {
		t.Fatal(err)
	}
	ann, bob := x.ann, x.bob
	cus := "ctm_" + x.tag
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(h int) time.Time { return t0.Add(time.Duration(h) * time.Hour) }
	good := signedCD(ann, checkoutSig(ann))

	// A bad signature is refused before anything is parsed; an unknown event is acknowledged.
	r := httptest.NewRequest("POST", "/api/billing/webhook", strings.NewReader(`{"event_type":"subscription.canceled"}`))
	r.Header.Set("Paddle-Signature", sign("pdl_wrong", time.Now().Unix(), []byte(`{"event_type":"subscription.canceled"}`)))
	w := httptest.NewRecorder()
	if x.mux.ServeHTTP(w, r); w.Code != 400 {
		t.Fatalf("bad signature: %d", w.Code)
	}
	r = httptest.NewRequest("POST", "/api/billing/webhook", strings.NewReader(`not json`))
	r.Header.Set("Paddle-Signature", sign(testSecret, time.Now().Unix(), []byte(`not json`)))
	w = httptest.NewRecorder()
	if x.mux.ServeHTTP(w, r); w.Code != 400 {
		t.Fatalf("signed garbage: %d", w.Code)
	}
	x.send(t, "payout.paid", `{"id":"pay_1"}`, 200)

	// Paddle.js checkout takes any custom_data: a forged, another user's or missing sig doesn't link.
	for i, cd := range []string{signedCD(ann, "00ff"), signedCD(ann, checkoutSig(bob)), fmt.Sprintf(`{"user_id":%q}`, ann), ""} {
		id := fmt.Sprintf("sub_forged%d_%s", i, x.tag)
		x.send(t, "subscription.created", sub{id: id, customer: cus, cd: cd, status: "active", price: "pri_adv_y", updated: at(0)}.json(), 200)
		if rw := x.row(t, id); rw.user != nil || x.plan(t, ann) != "free" {
			t.Fatalf("custom_data %s linked: %+v", cd, rw)
		}
	}

	// The subscription arrives before its customer: a placeholder customer, filled in later.
	sub1 := sub{id: "sub_1_" + x.tag, customer: "ctm_ann_" + x.tag, cd: good, status: "trialing", price: "pri_pro_m", updated: at(1)}
	x.send(t, "subscription.created", sub1.json(), 200)
	if rw := x.row(t, sub1.id); !is(rw.user, ann) || rw.plan != "pro" || rw.price != "pri_pro_m" || rw.product != "pro_pro" ||
		!is(rw.interval, "month") || !is(rw.currency, "inr") || rw.scheduled != nil || !rw.updated.Equal(at(1)) {
		t.Fatalf("after created: %+v", rw)
	}
	if u, email := x.customer(t, sub1.customer); !is(u, ann) || email != "" {
		t.Fatalf("placeholder customer: %v %q", u, email)
	}
	if p := x.plan(t, ann); p != "pro" {
		t.Fatalf("trialing plan %s", p)
	}
	for _, id := range []string{x.wsID, ws2} {
		var detail string
		var actor *string
		if err := x.pool.QueryRow(ctx, `select detail::text, actor_id::text from workspace_audit where workspace_id = $1 and action = 'billing.plan_changed'`, id).
			Scan(&detail, &actor); err != nil || !is(actor, ann) || detail != `{"to": "pro", "from": "free"}` {
			t.Fatalf("audit in %s: %s %v %v", id, detail, actor, err)
		}
	}
	cust := func(email string, h int) string {
		return fmt.Sprintf(`{"id":%q,"name":null,"email":%q,"status":"active","custom_data":null,"locale":"en",
			"created_at":"2026-01-01T00:00:00Z","updated_at":%q}`, sub1.customer, email, at(h).Format(time.RFC3339))
	}
	x.send(t, "customer.created", cust("ann@example.test", 1), 200)
	x.send(t, "customer.updated", cust("old@example.test", 0), 200) // older: ignored
	if u, email := x.customer(t, sub1.customer); !is(u, ann) || email != "ann@example.test" {
		t.Fatalf("customer filled in: %v %q", u, email)
	}

	// The same delivery twice changes nothing.
	x.send(t, "subscription.created", sub1.json(), 200)
	if n := x.audits(t, x.wsID); n != 1 || x.plan(t, ann) != "pro" {
		t.Fatalf("duplicate: %d audits, plan %s", n, x.plan(t, ann))
	}

	// A second signed subscription isn't linked while the first grants access.
	other := sub{id: "sub_other_" + x.tag, customer: sub1.customer, cd: good, status: "active", price: "pri_starter_m", updated: at(2)}
	x.send(t, "subscription.created", other.json(), 200)
	if rw := x.row(t, other.id); rw.user != nil || x.plan(t, ann) != "pro" {
		t.Fatalf("second subscription linked: %+v", rw)
	}

	// An upgrade: price, product and plan follow; unsigned, found by its id.
	sub1.cd, sub1.status, sub1.price, sub1.updated = "", "active", "pri_adv_y", at(3)
	x.send(t, "subscription.updated", sub1.json(), 200)
	if rw := x.row(t, sub1.id); rw.price != "pri_adv_y" || rw.product != "pro_adv" || rw.plan != "advanced" || !is(rw.interval, "year") ||
		x.plan(t, ann) != "advanced" {
		t.Fatalf("after upgrade: %+v %s", rw, x.plan(t, ann))
	}
	// Out of order: an older event doesn't overwrite.
	x.send(t, "subscription.updated", sub{id: sub1.id, customer: sub1.customer, status: "active", price: "pri_starter_m", updated: at(2)}.json(), 200)
	if rw := x.row(t, sub1.id); rw.plan != "advanced" || !rw.updated.Equal(at(3)) || x.plan(t, ann) != "advanced" {
		t.Fatalf("older event applied: %+v", rw)
	}

	// A scheduled cancel keeps access.
	sub1.cancel, sub1.updated = true, at(4)
	x.send(t, "subscription.updated", sub1.json(), 200)
	b := x.do(t, ann, "GET", "/api/billing", nil, 200)
	renews, err := time.Parse(time.RFC3339, fmt.Sprint(b["renews_at"]))
	if b["plan"] != "advanced" || b["status"] != "active" || b["cancel_at_period_end"] != true || b["interval"] != "year" ||
		b["currency"] != "inr" || err != nil || renews.Unix() != 1900000000 || x.row(t, sub1.id).scheduled == nil {
		t.Fatalf("scheduled cancel: %v", b)
	}
	sub1.cancel = false

	// past_due keeps access; paused denies it; resumed restores it.
	for _, c := range []struct{ typ, status, want string }{
		{"subscription.past_due", "past_due", "advanced"},
		{"subscription.paused", "paused", "free"},
		{"subscription.resumed", "active", "advanced"},
	} {
		sub1.status, sub1.updated = c.status, sub1.updated.Add(time.Hour)
		x.send(t, c.typ, sub1.json(), 200)
		if p := x.plan(t, ann); p != c.want {
			t.Fatalf("%s: plan %s, want %s", c.status, p, c.want)
		}
	}

	// An unknown plan on a subscription granting access is a 500 (Paddle retries); nothing changes.
	sub1.price, sub1.updated = "pri_nope", sub1.updated.Add(time.Hour)
	x.send(t, "subscription.updated", sub1.json(), 500)
	if rw := x.row(t, sub1.id); rw.price != "pri_adv_y" {
		t.Fatalf("price without taskwell_plan applied: %+v", rw)
	}

	// Canceled: Free, and GET /api/billing has no subscription.
	sub1.price, sub1.status = "pri_adv_y", "canceled"
	x.send(t, "subscription.canceled", sub1.json(), 200)
	if b := x.do(t, ann, "GET", "/api/billing", nil, 200); b["plan"] != "free" || b["status"] != nil || b["currency"] != nil ||
		b["interval"] != nil || b["renews_at"] != nil || b["cancel_at_period_end"] != false {
		t.Fatalf("after canceled: %v", b)
	}

	// A comped account keeps the admin's plan; a new signed subscription links now nothing else grants access.
	if _, err := x.pool.Exec(ctx, `update users set plan = 'advanced', plan_comped = true where id = $1`, ann); err != nil {
		t.Fatal(err)
	}
	sub2 := sub{id: "sub_2_" + x.tag, customer: sub1.customer, cd: good, status: "active", price: "pri_pro_y", updated: at(20)}
	x.send(t, "subscription.created", sub2.json(), 200)
	if rw := x.row(t, sub2.id); !is(rw.user, ann) || x.plan(t, ann) != "advanced" {
		t.Fatalf("comped: %+v %s", rw, x.plan(t, ann))
	}

	// transaction.completed makes and links the customer when its custom_data is signed, and only then.
	txn := func(customer, cd string) string {
		return fmt.Sprintf(`{"id":"txn_1","status":"completed","customer_id":%q,"subscription_id":null,"currency_code":"USD","custom_data":%s}`, customer, cd)
	}
	x.send(t, "transaction.completed", txn("ctm_bob_"+x.tag, signedCD(bob, checkoutSig(bob))), 200)
	if u, email := x.customer(t, "ctm_bob_"+x.tag); !is(u, bob) || email != "" {
		t.Fatalf("transaction customer: %v %q", u, email)
	}
	x.send(t, "transaction.completed", txn("ctm_eve_"+x.tag, signedCD(bob, "00ff")), 200)
	if u, _ := x.customer(t, "ctm_eve_"+x.tag); u != nil {
		t.Fatalf("forged transaction linked %v", *u)
	}
	x.send(t, "adjustment.created", `{"id":"adj_1","action":"refund","subscription_id":"sub_1","currency_code":"USD",
		"reason":"duplicate","totals":{"total":"1200"}}`, 200)
}

// TestEndpoints: checkout answers the account's signed custom_data and email (no Paddle call) while billing is on and
// it has no subscription granting access and isn't comped; the billing state and the portal and change read the
// mirrored subscription of the session's user; a workspace's view of its owner's plan.
func TestEndpoints(t *testing.T) {
	x := setup(t)
	ctx := context.Background()
	ann, bob, f, cfg := x.ann, x.bob, x.f, x.cfg
	base := "/api/billing"
	price := func(id string) url.Values { return url.Values{"price_id": {id}} }

	x.do(t, "", "GET", base, nil, 401)
	x.do(t, ann, "POST", base+"/checkout", nil, 400)
	x.do(t, ann, "POST", base+"/portal", nil, 409)
	x.do(t, ann, "POST", base+"/change", price("pri_pro_y"), 409)
	for _, env := range []string{"", "live", "Sandbox"} {
		cfg["paddle_environment"] = env
		x.do(t, ann, "POST", base+"/checkout", price("pri_pro_m"), 503)
		if b := x.do(t, ann, "GET", base, nil, 200); b["enabled"] != false {
			t.Fatalf("billing with environment %q: %v", env, b)
		}
	}
	cfg["paddle_environment"], cfg["paddle_api_key"] = "production", ""
	x.do(t, ann, "POST", base+"/checkout", price("pri_pro_m"), 503)
	cfg["paddle_environment"], cfg["paddle_api_key"] = "sandbox", "pdl_key"

	got := x.do(t, ann, "POST", base+"/checkout", price("pri_pro_m"), 200)
	if cd := got["custom_data"].(map[string]any); cd["user_id"] != ann || cd["sig"] != checkoutSig(ann) ||
		!strings.HasSuffix(got["email"].(string), "@dbtest.test") || len(got) != 2 {
		t.Fatalf("checkout %v", got)
	}
	if f.mu.Lock(); len(f.reqs) != 0 {
		t.Fatalf("checkout called Paddle: %v", f.reqs)
	}
	f.mu.Unlock()
	x.do(t, "", "GET", base+"/plans", nil, 404)

	if b := x.do(t, bob, "GET", base, nil, 200); b["plan"] != "free" || b["currency"] != nil || b["interval"] != nil || b["enabled"] != true ||
		b["status"] != nil || b["renews_at"] != nil || b["cancel_at_period_end"] != false || fmt.Sprint(b["workspaces"]) != "map[limit:1 used:0]" {
		t.Fatalf("Bob's billing %v", b)
	}

	wsBase := "/api/workspaces/" + x.slug + "/billing"
	if _, err := x.pool.Exec(ctx, `insert into projects (workspace_id, name, key) values ($1, 'Web', 'WEB')`, x.wsID); err != nil {
		t.Fatal(err)
	}
	v := x.do(t, bob, "GET", wsBase, nil, 200)
	if v["plan"] != "free" || v["is_owner"] != false || fmt.Sprint(v["owner"]) != "map[id:"+ann+" name:Ann]" ||
		fmt.Sprint(v["projects"]) != "map[limit:1 used:1]" || fmt.Sprint(v["members"]) != "map[limit:5 used:2]" ||
		fmt.Sprint(v["storage"]) != "map[limit_bytes:2.147483648e+09 used_bytes:0]" {
		t.Fatalf("Bob's view of the workspace %v", v)
	}
	x.do(t, bob, "GET", "/api/workspaces/no-such-ws/billing", nil, 404)
	if _, err := x.pool.Exec(ctx, `update workspace_members set role_id = (select id from workspace_roles where workspace_id = $1 and builtin = 'guest')
		where workspace_id = $1 and user_id = $2`, x.wsID, bob); err != nil {
		t.Fatal(err)
	}
	x.do(t, bob, "GET", wsBase, nil, 403)

	s := sub{id: "sub_" + x.tag, customer: "ctm_" + x.tag, cd: signedCD(ann, checkoutSig(ann)), status: "active", price: "pri_pro_m",
		updated: time.Now().Add(-time.Hour)}
	f.mu.Lock()
	f.subs[s.id] = s
	f.mu.Unlock()
	x.send(t, "subscription.created", s.json(), 200)
	if v := x.do(t, ann, "GET", wsBase, nil, 200); v["plan"] != "pro" || v["is_owner"] != true || fmt.Sprint(v["projects"]) != "map[limit:<nil> used:1]" {
		t.Fatalf("Ann's view of the workspace %v", v)
	}
	if b := x.do(t, ann, "GET", base, nil, 200); b["currency"] != "inr" || b["interval"] != "month" || b["status"] != "active" ||
		fmt.Sprint(b["workspaces"]) != "map[limit:3 used:1]" {
		t.Fatalf("Ann's billing %v", b)
	}
	x.do(t, ann, "POST", base+"/checkout", price("pri_adv_y"), 409)
	dbtest.SetPlan(t, x.pool, bob, "pro")
	if _, err := x.pool.Exec(ctx, `update users set plan_comped = true where id = $1`, bob); err != nil {
		t.Fatal(err)
	}
	x.do(t, bob, "POST", base+"/checkout", price("pri_pro_m"), 409) // comped
	x.do(t, bob, "POST", base+"/change", price("pri_adv_y"), 409)   // Bob's own account has no subscription
	// The portal's customer comes from the session's subscription only.
	x.do(t, bob, "POST", base+"/portal", url.Values{"customer_id": {s.customer}, "subscription_id": {s.id}}, 409)

	for _, p := range []string{"", "pri_missing", "pri_nope", "pri_old", "pri_pro_m"} {
		x.do(t, ann, "POST", base+"/change", price(p), 400)
	}
	if _, n := f.last("PATCH", "/subscriptions/"+s.id); n != 0 {
		t.Fatalf("a refused change patched the subscription")
	}
	if b := x.do(t, ann, "POST", base+"/change", price("pri_pro_y"), 200); b["plan"] != "pro" || b["interval"] != "year" ||
		b["status"] != "active" || b["currency"] != "inr" {
		t.Fatalf("interval change answered %v", b)
	}
	if patch, _ := f.last("PATCH", "/subscriptions/"+s.id); patch.body["proration_billing_mode"] != "prorated_immediately" ||
		fmt.Sprint(patch.body["items"]) != "[map[price_id:pri_pro_y quantity:1]]" {
		t.Fatalf("change body %v", patch.body)
	}
	if b := x.do(t, ann, "POST", base+"/change", price("pri_starter_m"), 200); b["plan"] != "starter" || b["interval"] != "month" {
		t.Fatalf("plan change answered %v", b)
	}
	if rw := x.row(t, s.id); rw.price != "pri_starter_m" || rw.plan != "starter" || x.plan(t, ann) != "starter" {
		t.Fatalf("after change: %+v", rw)
	}

	if got := x.do(t, ann, "POST", base+"/portal", url.Values{"customer_id": {"ctm_other"}}, 200)["url"]; got != "https://customer-portal.paddle.test/1" {
		t.Fatalf("portal %v", got)
	}
	if ps, n := f.last("POST", "/customers/"+s.customer+"/portal-sessions"); n != 1 || fmt.Sprint(ps.body["subscription_ids"]) != "["+s.id+"]" {
		t.Fatalf("portal body %v %d", ps.body, n)
	}
}

// TestRepeatTrial: one trial per account. GET /api/billing says when it's used; a later trialing subscription is
// activated (billed now) and mirrored active; a first trial is left alone.
func TestRepeatTrial(t *testing.T) {
	x := setup(t)
	ann, f := x.ann, x.f
	cd := signedCD(ann, checkoutSig(ann))
	if b := x.do(t, ann, "GET", "/api/billing", nil, 200); b["trial_used"] != false {
		t.Fatalf("new account: %v", b)
	}
	first := sub{id: "sub_first_" + x.tag, customer: "ctm_" + x.tag, cd: cd, status: "trialing", price: "pri_pro_m", updated: time.Now().Add(-time.Hour)}
	x.send(t, "subscription.created", first.json(), 200)
	if _, n := f.last("POST", "/subscriptions/"+first.id+"/activate"); n != 0 || x.row(t, first.id).status != "trialing" {
		t.Fatal("a first trial was ended")
	}
	first.status, first.updated = "canceled", first.updated.Add(time.Minute)
	x.send(t, "subscription.canceled", first.json(), 200)
	if b := x.do(t, ann, "GET", "/api/billing", nil, 200); b["trial_used"] != true || b["plan"] != "free" {
		t.Fatalf("after a canceled trial: %v", b)
	}

	again := sub{id: "sub_again_" + x.tag, customer: first.customer, cd: cd, status: "trialing", price: "pri_adv_y", updated: time.Now()}
	f.mu.Lock()
	f.subs[again.id] = again
	f.mu.Unlock()
	x.send(t, "subscription.created", again.json(), 200)
	if _, n := f.last("POST", "/subscriptions/"+again.id+"/activate"); n != 1 {
		t.Fatalf("repeat trial activated %d times", n)
	}
	if rw := x.row(t, again.id); rw.status != "active" || !is(rw.user, ann) || x.plan(t, ann) != "advanced" {
		t.Fatalf("repeat trial: %+v %s", rw, x.plan(t, ann))
	}
	// Paddle's own activated event, and a late copy of the trialing one, change nothing more.
	x.send(t, "subscription.trialing", again.json(), 200)
	if _, n := f.last("POST", "/subscriptions/"+again.id+"/activate"); n != 1 || x.row(t, again.id).status != "active" {
		t.Fatal("a late trialing event re-ran the activation")
	}
}

// TestChangeInTrial: Paddle only takes do_not_bill for a trialing subscription's items, so a switch in a trial uses it.
func TestChangeInTrial(t *testing.T) {
	x := setup(t)
	s := sub{id: "sub_trial_" + x.tag, customer: "ctm_" + x.tag, cd: signedCD(x.ann, checkoutSig(x.ann)), status: "trialing", price: "pri_starter_m",
		updated: time.Now().Add(-time.Hour)}
	x.f.mu.Lock()
	x.f.subs[s.id] = s
	x.f.mu.Unlock()
	x.send(t, "subscription.created", s.json(), 200)
	if b := x.do(t, x.ann, "POST", "/api/billing/change", url.Values{"price_id": {"pri_pro_m"}}, 200); b["plan"] != "pro" || b["status"] != "trialing" {
		t.Fatalf("change in trial answered %v", b)
	}
	if patch, _ := x.f.last("PATCH", "/subscriptions/"+s.id); patch.body["proration_billing_mode"] != "do_not_bill" {
		t.Fatalf("change in trial body %v", patch.body)
	}
}
