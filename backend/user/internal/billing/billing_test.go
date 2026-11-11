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
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/mail"
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
	"pri_starter_m": `{"id":"pri_starter_m","product_id":"pro_starter","status":"active","custom_data":{"taskwell_plan":"starter"},"billing_cycle":{"interval":"month","frequency":1},"unit_price":{"amount":"90000","currency_code":"INR"}}`,
	"pri_pro_m":     `{"id":"pri_pro_m","product_id":"pro_pro","status":"active","custom_data":{"taskwell_plan":"pro"},"billing_cycle":{"interval":"month","frequency":1},"unit_price":{"amount":"150000","currency_code":"INR"}}`,
	"pri_pro_y":     `{"id":"pri_pro_y","product_id":"pro_pro","status":"active","custom_data":{"taskwell_plan":"pro"},"billing_cycle":{"interval":"year","frequency":1},"unit_price":{"amount":"1500000","currency_code":"INR"}}`,
	"pri_adv_y":     `{"id":"pri_adv_y","product_id":"pro_adv","status":"active","custom_data":{"taskwell_plan":"advanced"},"billing_cycle":{"interval":"year","frequency":1},"unit_price":{"amount":"3000000","currency_code":"INR"}}`,
	"pri_nope":      `{"id":"pri_nope","product_id":"pro_x","status":"active","custom_data":null,"billing_cycle":{"interval":"month","frequency":1},"unit_price":{"amount":"100","currency_code":"INR"}}`,
	"pri_old":       `{"id":"pri_old","product_id":"pro_starter","status":"archived","custom_data":{"taskwell_plan":"starter"},"billing_cycle":{"interval":"month","frequency":1},"unit_price":{"amount":"80000","currency_code":"INR"}}`,
}

// sub is a Paddle subscription entity; cd is its custom_data JSON ("" for null).
type sub struct {
	id, customer, cd, status, price string
	updated                         time.Time
	cancel                          bool
	cardless                        bool // a trial without a payment method: no next_billed_at, trial_dates on the item
}

func (s sub) json() string {
	sc, cd := "null", "null"
	if s.cancel {
		sc = `{"action":"cancel","effective_at":"2030-03-17T17:46:40Z","resume_at":null}`
	}
	if s.cd != "" {
		cd = s.cd
	}
	next, trial := `"2030-03-17T17:46:40Z"`, "null"
	if s.cardless {
		next, trial = "null", `{"starts_at":"2030-03-03T17:46:40Z","ends_at":"2030-03-17T17:46:40Z"}`
	}
	return fmt.Sprintf(`{"id":%q,"status":%q,"customer_id":%q,"address_id":"add_1","currency_code":"INR",
		"created_at":"2026-01-01T00:00:00Z","updated_at":%q,"next_billed_at":%s,
		"billing_cycle":{"interval":"month","frequency":1},"scheduled_change":%s,"custom_data":%s,
		"items":[{"status":"active","quantity":1,"trial_dates":%s,"price":%s}]}`,
		s.id, s.status, s.customer, s.updated.Format(time.RFC3339Nano), next, sc, cd, trial, prices[s.price])
}

// fakePaddle answers the Paddle calls billing makes (price reads, subscription patches, portal sessions) and records
// each request.
type fakePaddle struct {
	mu         sync.Mutex
	subs       map[string]sub
	reqs       []paddleReq
	txns, adjs []string // what GET /transactions and /adjustments list, two a page
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
	page := func(all []string) {
		i, _ := strconv.Atoi(r.URL.Query().Get("after"))
		j := min(i+2, len(all))
		next := fmt.Sprintf("https://api.paddle.test%s?after=%d", r.URL.Path, j)
		fmt.Fprintf(w, `{"data":[%s],"meta":{"request_id":"r1","pagination":{"per_page":2,"next":%q,"has_more":%v}}}`,
			strings.Join(all[i:j], ","), next, j < len(all))
	}
	switch p := r.URL.Path; {
	case p == "/transactions" && r.Method == http.MethodGet:
		page(f.txns)
	case p == "/adjustments" && r.Method == http.MethodGet:
		page(f.adjs)
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
	case isSub && known && r.Method == http.MethodGet:
		reply(s.json())
	case isSub && r.Method == http.MethodGet && strings.HasSuffix(id, "/update-payment-method-transaction"):
		reply(`{"id":"txn_pm_1","status":"ready","origin":"subscription_payment_method_change"}`)
	case strings.HasSuffix(p, "/cancel") && r.Method == http.MethodPost:
		id := strings.TrimSuffix(id, "/cancel")
		s := f.subs[id]
		s.status, s.updated = "canceled", s.updated.Add(time.Minute)
		f.subs[id] = s
		reply(s.json())
	case isSub && r.Method == http.MethodPatch && strings.HasSuffix(id, "/preview"):
		// As Paddle answers: no update_summary in a trial, a prorated charge otherwise.
		summary := `{"result":{"action":"charge","amount":"1234","currency_code":"INR"}}`
		if f.subs[strings.TrimSuffix(id, "/preview")].status == "trialing" {
			summary = "null"
		}
		reply(`{"status":"` + f.subs[strings.TrimSuffix(id, "/preview")].status + `","currency_code":"INR","next_billed_at":"2030-03-17T17:46:40Z",
			"update_summary":` + summary + `,"recurring_transaction_details":{"totals":{"total":"150000"}}}`)
	// A trialing subscription takes do_not_bill only; changing its billing cycle with another mode is refused with a
	// different code than changing its plan (as the sandbox does).
	case isSub && known && r.Method == http.MethodPatch && s.status == "trialing" && body["proration_billing_mode"] != "do_not_bill":
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":{"type":"request_error","code":"subscription_new_items_not_valid","detail":"use do_not_bill"}}`)
	case isSub && known && r.Method == http.MethodPatch && s.status == "locked":
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":{"type":"request_error","code":"subscription_locked_renewal","detail":"renews soon"}}`)
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
	h                    *Handler
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
		_, _ = x.pool.Exec(context.Background(), `delete from paddle_transactions where transaction_id like '%'||$1||'%'`, x.tag)
		_, _ = x.pool.Exec(context.Background(), `delete from paddle_adjustments where adjustment_id like '%'||$1||'%'`, x.tag)
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
	cfg := sysconfig.New(x.pool)
	h := New(x.pool, cfg, mail.New(x.pool, cfg, time.Second, time.Second, 1), testJWT, dbtest.Auth,
		workspace.New(x.pool, dbtest.Auth, nil, cfg).Member)
	x.h = h
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
	var unit *int64
	if err := x.pool.QueryRow(ctx, `select unit_amount from paddle_subscriptions where subscription_id = $1`, sub1.id).Scan(&unit); err != nil ||
		!is64(unit, 150000) {
		t.Fatalf("unit_amount %v %v", unit, err)
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

	// transaction.completed makes and links the customer when its custom_data is signed, and only then, and stores
	// the payment for the account.
	txn := func(id, customer, cd string) string {
		return txnJSON(id, customer, cd, "completed", "1200", "2030-01-02T00:00:00Z")
	}
	x.send(t, "transaction.completed", txn("txn_1_"+x.tag, "ctm_bob_"+x.tag, signedCD(bob, checkoutSig(bob))), 200)
	if u, email := x.customer(t, "ctm_bob_"+x.tag); !is(u, bob) || email != "" {
		t.Fatalf("transaction customer: %v %q", u, email)
	}
	if tr := x.txn(t, "txn_1_"+x.tag); !is(tr.user, bob) || tr.currency != "usd" || tr.subtotal != 1000 || tr.tax != 200 ||
		tr.total != 1200 || !is64(tr.earnings, 1100) || tr.status != "completed" || tr.billed.UTC().Format(time.RFC3339) != "2030-01-02T00:00:00Z" {
		t.Fatalf("stored transaction: %+v", tr)
	}
	x.send(t, "transaction.completed", txn("txn_2_"+x.tag, "ctm_eve_"+x.tag, signedCD(bob, "00ff")), 200)
	if u, _ := x.customer(t, "ctm_eve_"+x.tag); u != nil {
		t.Fatalf("forged transaction linked %v", *u)
	}
	if tr := x.txn(t, "txn_2_"+x.tag); tr.user != nil {
		t.Fatalf("forged transaction's user %v", *tr.user)
	}
	// A paid transaction is stored; one still billed isn't.
	x.send(t, "transaction.paid", txnJSON("txn_3_"+x.tag, "ctm_bob_"+x.tag, "null", "paid", "500", "2030-01-03T00:00:00Z"), 200)
	if tr := x.txn(t, "txn_3_"+x.tag); !is(tr.user, bob) || tr.status != "paid" || tr.total != 500 {
		t.Fatalf("paid transaction (user from its customer): %+v", tr)
	}
	x.send(t, "transaction.updated", txnJSON("txn_4_"+x.tag, "ctm_bob_"+x.tag, "null", "billed", "500", "2030-01-03T00:00:00Z"), 200)
	var n int
	if err := x.pool.QueryRow(ctx, `select count(*) from paddle_transactions where transaction_id = $1`, "txn_4_"+x.tag).Scan(&n); err != nil || n != 0 {
		t.Fatalf("billed transaction stored: %d %v", n, err)
	}

	// An adjustment is stored with its transaction's account; a later update moves its status, an older one doesn't.
	adj := func(status, updated string) string {
		return fmt.Sprintf(`{"id":%q,"action":"refund","transaction_id":%q,"customer_id":%q,"subscription_id":null,
			"currency_code":"USD","status":%q,"reason":"duplicate","created_at":"2030-01-04T00:00:00Z","updated_at":%q,
			"totals":{"subtotal":"1000","tax":"200","total":"1200","fee":"60","earnings":"940","currency_code":"USD"}}`,
			"adj_1_"+x.tag, "txn_1_"+x.tag, "ctm_bob_"+x.tag, status, updated)
	}
	x.send(t, "adjustment.created", adj("pending_approval", "2030-01-04T00:00:00Z"), 200)
	x.send(t, "adjustment.updated", adj("approved", "2030-01-05T00:00:00Z"), 200)
	x.send(t, "adjustment.updated", adj("rejected", "2030-01-04T12:00:00Z"), 200)
	var au *string
	var action, status, cur string
	var total int64
	if err := x.pool.QueryRow(ctx, `select user_id::text, action, status, currency_code, total from paddle_adjustments where adjustment_id = $1`,
		"adj_1_"+x.tag).Scan(&au, &action, &status, &cur, &total); err != nil || !is(au, bob) || action != "refund" ||
		status != "approved" || cur != "usd" || total != 1200 {
		t.Fatalf("adjustment: %v %s %s %s %d %v", au, action, status, cur, total, err)
	}
}

// txnJSON is a Paddle transaction entity with details.totals in USD: total split 5/6 subtotal, 1/6 tax; earnings
// total minus 100.
func txnJSON(id, customer, cd, status, total, billed string) string {
	n, _ := amount(total)
	return fmt.Sprintf(`{"id":%q,"status":%q,"customer_id":%q,"subscription_id":null,"currency_code":"USD","custom_data":%s,
		"origin":"web","billed_at":%q,"created_at":"2030-01-01T00:00:00Z","updated_at":%q,
		"details":{"totals":{"subtotal":"%d","discount":"0","tax":"%d","total":%q,"credit":"0","balance":"0",
		"grand_total":%q,"fee":"100","earnings":"%d","currency_code":"USD"}}}`,
		id, status, customer, cd, billed, billed, n*5/6, n/6, total, total, n-100)
}

// storedTxn is a mirrored transaction.
type storedTxn struct {
	user                 *string
	status, currency     string
	subtotal, tax, total int64
	earnings             *int64
	billed               time.Time
}

func (x *fixture) txn(t *testing.T, id string) storedTxn {
	t.Helper()
	var r storedTxn
	if err := x.pool.QueryRow(context.Background(), `select user_id::text, status, currency_code, subtotal, tax, total, earnings,
		billed_at from paddle_transactions where transaction_id = $1`, id).
		Scan(&r.user, &r.status, &r.currency, &r.subtotal, &r.tax, &r.total, &r.earnings, &r.billed); err != nil {
		t.Fatalf("transaction %s: %v", id, err)
	}
	return r
}

func is64(p *int64, want int64) bool { return p != nil && *p == want }

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
	if code, body := dbtest.Do(t, x.mux, "", "GET", "/api/plans", nil); code != 200 ||
		!strings.Contains(body, `{"ai_usd":3,"id":"pro","members":25,"projects":0,"storage_gb":100,"workspaces":3}`) {
		t.Fatalf("plans %d %s", code, body)
	}

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
		fmt.Sprint(v["projects"]) != "map[limit:1 used:1]" || fmt.Sprint(v["members"]) != "map[limit:1 used:2]" ||
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

// TestChangeInTrial: Paddle's own status of the subscription picks the mode: do_not_bill in a trial (a plan or an
// interval change), prorated_immediately after it; a past_due one, and one Paddle refuses, answer 409 unchanged.
func TestChangeInTrial(t *testing.T) {
	x := setup(t)
	s := sub{id: "sub_trial_" + x.tag, customer: "ctm_" + x.tag, cd: signedCD(x.ann, checkoutSig(x.ann)), status: "trialing", price: "pri_starter_m",
		updated: time.Now().Add(-time.Hour)}
	set := func(status string) {
		x.f.mu.Lock()
		defer x.f.mu.Unlock()
		if cur, ok := x.f.subs[s.id]; ok {
			s = cur
		}
		s.status = status
		x.f.subs[s.id] = s
	}
	price := func(id string) url.Values { return url.Values{"price_id": {id}} }
	patches := func(n int, mode string) {
		t.Helper()
		if patch, got := x.f.last("PATCH", "/subscriptions/"+s.id); got != n || patch.body["proration_billing_mode"] != mode {
			t.Fatalf("%d patches, last %v; want %d with %s", got, patch.body, n, mode)
		}
	}
	set("trialing")
	x.send(t, "subscription.created", s.json(), 200)
	if p := x.do(t, x.ann, "POST", "/api/billing/change/preview", price("pri_pro_y"), 200); p["trialing"] != true || p["today"] != nil ||
		p["recurring"] != "150000" || p["currency"] != "INR" || p["next_billed_at"] != "2030-03-17T17:46:40Z" {
		t.Fatalf("preview in trial answered %v", p)
	}
	if _, n := x.f.last("PATCH", "/subscriptions/"+s.id); n != 0 || x.row(t, s.id).price != "pri_starter_m" {
		t.Fatal("a preview changed the subscription")
	}
	if b := x.do(t, x.ann, "POST", "/api/billing/change", price("pri_pro_y"), 200); b["plan"] != "pro" || b["status"] != "trialing" || b["interval"] != "year" {
		t.Fatalf("change in trial answered %v", b)
	}
	patches(1, "do_not_bill")
	// The mirror still says trialing but Paddle has billed it: Paddle's answer wins, so it's prorated.
	set("active")
	if p := x.do(t, x.ann, "POST", "/api/billing/change/preview", price("pri_starter_m"), 200); p["trialing"] != false ||
		fmt.Sprint(p["today"]) != "map[action:charge amount:1234]" {
		t.Fatalf("preview after the trial answered %v", p)
	}
	x.do(t, x.ann, "POST", "/api/billing/change", price("pri_starter_m"), 200)
	patches(2, "prorated_immediately")
	set("past_due")
	x.do(t, x.ann, "POST", "/api/billing/change", price("pri_pro_m"), 409)
	x.do(t, x.ann, "POST", "/api/billing/change/preview", price("pri_pro_m"), 409)
	patches(2, "prorated_immediately")
	set("locked")
	x.do(t, x.ann, "POST", "/api/billing/change", price("pri_pro_m"), 409)
	if x.row(t, s.id).price != "pri_starter_m" {
		t.Fatal("a refused change moved the mirror")
	}
}

// TestCardlessTrial: a trial started without a payment method has no next billing, so the state carries its end and
// says a payment method is needed; POST payment-method answers Paddle's transaction for adding one; a repeat trial
// without one is canceled, not activated.
func TestCardlessTrial(t *testing.T) {
	x := setup(t)
	ann, f := x.ann, x.f
	cd := signedCD(ann, checkoutSig(ann))
	x.do(t, ann, "POST", "/api/billing/payment-method", nil, 409) // no subscription
	first := sub{id: "sub_cl_" + x.tag, customer: "ctm_" + x.tag, cd: cd, status: "trialing", price: "pri_pro_m", cardless: true,
		updated: time.Now().Add(-time.Hour)}
	x.send(t, "subscription.created", first.json(), 200)
	b := x.do(t, ann, "GET", "/api/billing", nil, 200)
	if ends, _ := time.Parse(time.RFC3339, fmt.Sprint(b["trial_ends_at"])); b["plan"] != "pro" || b["needs_payment_method"] != true ||
		!ends.Equal(time.Date(2030, 3, 17, 17, 46, 40, 0, time.UTC)) || b["renews_at"] != nil {
		t.Fatalf("cardless trial: %v", b)
	}
	if b := x.do(t, ann, "POST", "/api/billing/payment-method", nil, 200); b["transaction_id"] != "txn_pm_1" {
		t.Fatalf("payment method: %v", b)
	}
	// The card is added: Paddle sets the next billing.
	first.cardless, first.updated = false, first.updated.Add(time.Minute)
	x.send(t, "subscription.updated", first.json(), 200)
	if b := x.do(t, ann, "GET", "/api/billing", nil, 200); b["needs_payment_method"] != false || b["renews_at"] == nil {
		t.Fatalf("after adding a card: %v", b)
	}
	first.status, first.updated = "canceled", first.updated.Add(time.Minute)
	x.send(t, "subscription.canceled", first.json(), 200)
	if b := x.do(t, ann, "GET", "/api/billing", nil, 200); b["plan"] != "free" || b["needs_payment_method"] != false || b["trial_ends_at"] != nil {
		t.Fatalf("after the trial was canceled: %v", b)
	}

	again := sub{id: "sub_cl2_" + x.tag, customer: first.customer, cd: cd, status: "trialing", price: "pri_adv_y", cardless: true, updated: time.Now()}
	f.mu.Lock()
	f.subs[again.id] = again
	f.mu.Unlock()
	x.send(t, "subscription.created", again.json(), 200)
	_, activated := f.last("POST", "/subscriptions/"+again.id+"/activate")
	if cancel, n := f.last("POST", "/subscriptions/"+again.id+"/cancel"); n != 1 || activated != 0 || cancel.body["effective_from"] != "immediately" {
		t.Fatalf("repeat cardless trial: %d cancels %v, %d activations", n, cancel.body, activated)
	}
	if rw := x.row(t, again.id); rw.status != "canceled" || x.plan(t, ann) != "free" {
		t.Fatalf("repeat cardless trial: %+v %s", rw, x.plan(t, ann))
	}
}

// TestCompEnds: a comp that's over goes back to Free, or to the plan of the subscription granting access, with an
// audit entry in the owner's workspace and an email; one that isn't over is left alone.
func TestCompEnds(t *testing.T) {
	x := setup(t)
	ctx := context.Background()
	x.cfg["app_base_url"] = "https://app.test/"
	var ann string
	if err := x.pool.QueryRow(ctx, `select email from users where id = $1`, x.ann).Scan(&ann); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = x.pool.Exec(context.Background(), `delete from outbound_mail where recipient = $1`, ann)
	})
	comp := func(user, p, ends string) {
		t.Helper()
		if _, err := x.pool.Exec(ctx, `update users set plan = $2, plan_comped = true, plan_comped_until = now() + $3::interval
			where id = $1`, user, p, ends); err != nil {
			t.Fatal(err)
		}
	}
	run := func() {
		t.Helper()
		for {
			ended, err := x.h.endComp(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !ended {
				return
			}
		}
	}
	state := func(user string) (p string, comped bool, until *time.Time) {
		t.Helper()
		if err := x.pool.QueryRow(ctx, `select plan, plan_comped, plan_comped_until from users where id = $1`, user).
			Scan(&p, &comped, &until); err != nil {
			t.Fatal(err)
		}
		return p, comped, until
	}
	mailed := func(subject, want string) {
		t.Helper()
		var body string
		if err := x.pool.QueryRow(ctx, `select body from outbound_mail where recipient = $1 and subject = $2 and html is not null`,
			ann, subject).Scan(&body); err != nil || !strings.Contains(body, want) || !strings.Contains(body, "https://app.test/pricing") {
			t.Fatalf("email %q: %q %v", subject, body, err)
		}
	}

	comp(x.ann, "advanced", "-1 minute")
	comp(x.bob, "pro", "1 day")
	run()
	if p, comped, until := state(x.ann); p != "free" || comped || until != nil {
		t.Fatalf("ended comp: %s %v %v", p, comped, until)
	}
	if p, comped, until := state(x.bob); p != "pro" || !comped || until == nil {
		t.Fatalf("comp not over: %s %v %v", p, comped, until)
	}
	if n := x.audits(t, x.wsID); n != 1 {
		t.Fatalf("audits %d", n)
	}
	mailed("Your Taskwell Advanced access has ended", "You're on Free now.")

	cus := "ctm_" + x.tag
	if _, err := x.pool.Exec(ctx, `insert into paddle_customers (customer_id, user_id) values ($1, $2)`, cus, x.ann); err != nil {
		t.Fatal(err)
	}
	if _, err := x.pool.Exec(ctx, `insert into paddle_subscriptions (subscription_id, customer_id, user_id, status, price_id, product_id, plan, paddle_updated_at)
		values ($1, $2, $3, 'active', 'pri_1', 'pro_1', 'starter', now())`, "sub_"+x.tag, cus, x.ann); err != nil {
		t.Fatal(err)
	}
	comp(x.ann, "pro", "-1 second")
	run()
	if p, comped, _ := state(x.ann); p != "starter" || comped {
		t.Fatalf("ended with a subscription: %s %v", p, comped)
	}
	mailed("Your Taskwell Pro access has ended", "You're on Starter now.")
	if b := x.do(t, x.bob, "GET", "/api/billing", nil, 200); b["comped_until"] == nil {
		t.Fatalf("billing answered %v", b)
	}
}

// TestReminders: each kind of billing reminder (trial ending, payment failed, comp ending) queues one email once; a
// trial set to cancel gets none; the payment failed marker resets once the subscription is no longer past due.
func TestReminders(t *testing.T) {
	x := setup(t)
	ctx := context.Background()
	x.cfg["app_base_url"] = "https://app.test/"
	var ann, bob string
	if err := x.pool.QueryRow(ctx, `select (select email from users where id = $1), (select email from users where id = $2)`,
		x.ann, x.bob).Scan(&ann, &bob); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = x.pool.Exec(context.Background(), `delete from outbound_mail where recipient in ($1, $2)`, ann, bob)
	})
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := x.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	cus := "ctm_" + x.tag
	exec(`insert into paddle_customers (customer_id, user_id) values ($1, $2)`, cus, x.ann)
	subscribe := func(id, user, status, p, cancel string) {
		t.Helper()
		exec(`insert into paddle_subscriptions (subscription_id, customer_id, user_id, status, price_id, product_id, plan,
			next_billed_at, scheduled_change_action, paddle_updated_at)
			values ($1, $2, $3, $4, 'pri_1', 'pro_1', $5, now() + interval '2 days', nullif($6, ''), now())`, id, cus, user, status, p, cancel)
	}
	subscribe("sub_trial_"+x.tag, x.ann, "trialing", "pro", "")
	subscribe("sub_cancel_"+x.tag, x.bob, "trialing", "starter", "cancel")
	subscribe("sub_due_"+x.tag, x.ann, "past_due", "starter", "")
	exec(`update users set plan = 'advanced', plan_comped = true, plan_comped_until = now() + interval '1 day' where id = $1`, x.bob)
	var trialDay, compDay string
	if err := x.pool.QueryRow(ctx, `select to_char((now() + interval '2 days') at time zone 'UTC', 'Mon FMDD'),
		to_char((select plan_comped_until from users where id = $1) at time zone 'UTC', 'Mon FMDD')`, x.bob).
		Scan(&trialDay, &compDay); err != nil {
		t.Fatal(err)
	}
	run := func() {
		t.Helper()
		if err := x.h.resetPastDue(ctx); err != nil {
			t.Fatal(err)
		}
		for {
			sent, err := x.h.remind(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !sent {
				return
			}
		}
	}
	count := func(to, subject string) int {
		t.Helper()
		var n int
		if err := x.pool.QueryRow(ctx, `select count(*) from outbound_mail where recipient = $1 and subject = $2`, to, subject).
			Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	emails := []struct{ to, subject, body, link string }{
		{ann, "Your Taskwell Pro trial ends on " + trialDay, "You'll be charged for Pro on " + trialDay + " unless you cancel.", "https://app.test/billing"},
		{ann, "Your Taskwell payment failed", "We couldn't take your payment for Starter.", "https://app.test/billing"},
		{bob, "Your Taskwell Advanced access ends on " + compDay, "Choose a plan to keep its features.", "https://app.test/pricing"},
	}
	check := func(want int) {
		t.Helper()
		for _, e := range emails {
			var body string
			var n int
			if err := x.pool.QueryRow(ctx, `select count(*), coalesce(min(body), '') from outbound_mail
				where recipient = $1 and subject = $2 and html is not null`, e.to, e.subject).Scan(&n, &body); err != nil {
				t.Fatal(err)
			}
			if n != want || want > 0 && (!strings.Contains(body, e.body) || !strings.Contains(body, e.link)) {
				t.Fatalf("%q to %s: %d emails, want %d: %q", e.subject, e.to, n, want, body)
			}
		}
	}
	run()
	check(1)
	run()
	check(1)
	if n := count(bob, "Your Taskwell Starter trial ends on "+trialDay); n != 0 {
		t.Fatalf("a trial set to cancel got %d reminders", n)
	}
	// A trial without a payment method has no next billing: its own reminder goes by the trial's end, once.
	exec(`update paddle_subscriptions set scheduled_change_action = null, next_billed_at = null, trial_ends_at = now() + interval '2 days'
		where subscription_id = $1`, "sub_cancel_"+x.tag)
	run()
	run()
	var cardless string
	if err := x.pool.QueryRow(ctx, `select coalesce(min(body), '') from outbound_mail where recipient = $1 and subject = $2`,
		bob, "Your Taskwell Starter trial ends on "+trialDay).Scan(&cardless); err != nil {
		t.Fatal(err)
	}
	if n := count(bob, "Your Taskwell Starter trial ends on "+trialDay); n != 1 || !strings.Contains(cardless, "Add a payment method to keep Starter") {
		t.Fatalf("cardless trial reminder: %d emails %q", n, cardless)
	}

	exec(`update paddle_subscriptions set status = 'active' where subscription_id = $1`, "sub_due_"+x.tag)
	run()
	var marker *time.Time
	if err := x.pool.QueryRow(ctx, `select past_due_emailed_at from paddle_subscriptions where subscription_id = $1`,
		"sub_due_"+x.tag).Scan(&marker); err != nil || marker != nil {
		t.Fatalf("past_due marker kept: %v %v", marker, err)
	}
	exec(`update paddle_subscriptions set status = 'past_due' where subscription_id = $1`, "sub_due_"+x.tag)
	run()
	if n := count(ann, "Your Taskwell payment failed"); n != 2 {
		t.Fatalf("a second past due episode: %d emails, want 2", n)
	}
}

// TestBackfill: -paddle-backfill pages through Paddle's transactions and adjustments and fills in the unit amount of
// mirrored subscriptions without one; a second run changes nothing.
func TestBackfill(t *testing.T) {
	x := setup(t)
	ctx := context.Background()
	cus := "ctm_bf_" + x.tag
	if _, err := x.pool.Exec(ctx, `insert into paddle_customers (customer_id, user_id) values ($1, $2)`, cus, x.bob); err != nil {
		t.Fatal(err)
	}
	if _, err := x.pool.Exec(ctx, `insert into paddle_subscriptions (subscription_id, customer_id, user_id, status, price_id,
		product_id, plan, billing_interval, paddle_updated_at) values ($1, $2, $3, 'active', 'pri_pro_y', 'pro_pro', 'pro', 'year', now())`,
		"sub_bf_"+x.tag, cus, x.bob); err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		x.f.txns = append(x.f.txns, txnJSON(fmt.Sprintf("txn_bf%d_%s", i, x.tag), cus, "null", "completed", "900", "2030-02-01T00:00:00Z"))
	}
	x.f.adjs = []string{fmt.Sprintf(`{"id":"adj_bf_%s","action":"chargeback","transaction_id":"txn_bf0_%s","customer_id":%q,
		"currency_code":"USD","status":"approved","created_at":"2030-02-02T00:00:00Z","updated_at":"2030-02-02T00:00:00Z",
		"totals":{"total":"900","currency_code":"USD"}}`, x.tag, x.tag, cus)}

	// The test database may hold other subscriptions without an amount: the first run sets at least ours.
	for run, want := range []BackfillCounts{{3, 1, 1}, {3, 1, 0}} {
		got, err := x.h.Backfill(ctx)
		if err != nil || got.Transactions != want.Transactions || got.Adjustments != want.Adjustments ||
			(want.Subscriptions == 0) != (got.Subscriptions == 0) {
			t.Fatalf("run %d: %+v %v, want %+v", run, got, err, want)
		}
	}
	if _, n := x.f.last("GET", "/transactions"); n != 4 { // two pages, twice
		t.Fatalf("%d transaction pages", n)
	}
	var txns int
	var sum, unit int64
	var adjUser *string
	if err := x.pool.QueryRow(ctx, `select (select count(*) from paddle_transactions where transaction_id like 'txn_bf%'||$1),
		(select sum(total) from paddle_transactions where transaction_id like 'txn_bf%'||$1 and user_id = $2),
		(select unit_amount from paddle_subscriptions where subscription_id = 'sub_bf_'||$1),
		(select user_id::text from paddle_adjustments where adjustment_id = 'adj_bf_'||$1)`, x.tag, x.bob).
		Scan(&txns, &sum, &unit, &adjUser); err != nil || txns != 3 || sum != 2700 || unit != 1500000 || !is(adjUser, x.bob) {
		t.Fatalf("after backfill: %d txns, %d, unit %d, adjustment user %v, %v", txns, sum, unit, adjUser, err)
	}
}
