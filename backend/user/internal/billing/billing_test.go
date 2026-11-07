package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

const testSecret = "whsec_test"

func sign(secret string, ts int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.%s", ts, body)
	return fmt.Sprintf("t=%d,v1=%s", ts, hex.EncodeToString(mac.Sum(nil)))
}

func TestVerifySignature(t *testing.T) {
	body := []byte(`{"id":"evt_1"}`)
	now := time.Unix(1_800_000_000, 0)
	good := sign(testSecret, now.Unix(), body)
	for _, c := range []struct {
		name, header string
		body         []byte
		want         bool
	}{
		{"good", good, body, true},
		{"second v1 matches", fmt.Sprintf("t=%d,v1=00ff,%s", now.Unix(), strings.Split(good, ",")[1]), body, true},
		{"wrong secret", sign("whsec_other", now.Unix(), body), body, false},
		{"tampered body", good, []byte(`{"id":"evt_2"}`), false},
		{"stale", sign(testSecret, now.Add(-6*time.Minute).Unix(), body), body, false},
		{"future", sign(testSecret, now.Add(6*time.Minute).Unix(), body), body, false},
		{"only v0", strings.Replace(good, "v1=", "v0=", 1), body, false},
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

// fakeStripe answers the Stripe calls billing makes and records each request.
type fakeStripe struct {
	mu   sync.Mutex
	subs map[string]string // subscription id → its JSON
	// chargeCustomer is every charge's customer.
	chargeCustomer string
	reqs           []stripeReq
}

type stripeReq struct {
	method, path, idem string
	form               url.Values
}

const pricesJSON = `{"data":[
	{"id":"price_pro","lookup_key":"pro_monthly","currency":"usd","unit_amount":2900,
	 "currency_options":{"usd":{"unit_amount":2900},"inr":{"unit_amount":149900}}},
	{"id":"price_ult","lookup_key":"ultimate_monthly","currency":"usd","unit_amount":7900,
	 "currency_options":{"inr":{"unit_amount":399900}}}]}`

func (f *fakeStripe) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer rk_test" || r.Header.Get("Stripe-Version") != stripeVersion {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"type":"invalid_request_error","message":"bad key"}}`)
		return
	}
	_ = r.ParseForm()
	form := r.PostForm
	if r.Method == http.MethodGet {
		form = r.URL.Query()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, stripeReq{r.Method, r.URL.Path, r.Header.Get("Idempotency-Key"), form})
	switch p := r.URL.Path; {
	case p == "/v1/prices":
		fmt.Fprint(w, pricesJSON)
	case p == "/v1/customers":
		fmt.Fprint(w, `{"id":"cus_new"}`)
	case p == "/v1/checkout/sessions":
		fmt.Fprint(w, `{"id":"cs_1","url":"https://checkout.stripe.test/cs_1"}`)
	case p == "/v1/billing_portal/sessions":
		fmt.Fprint(w, `{"id":"bps_1","url":"https://billing.stripe.test/p/1"}`)
	case strings.HasPrefix(p, "/v1/subscriptions/") && f.subs[strings.TrimPrefix(p, "/v1/subscriptions/")] != "":
		fmt.Fprint(w, f.subs[strings.TrimPrefix(p, "/v1/subscriptions/")])
	case strings.HasPrefix(p, "/v1/charges/"):
		fmt.Fprintf(w, `{"id":"ch_1","customer":%q}`, f.chargeCustomer)
	default:
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":{"type":"invalid_request_error","message":"no such object"}}`)
	}
}

func (f *fakeStripe) last(path string) stripeReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.reqs) - 1; i >= 0; i-- {
		if f.reqs[i].path == path {
			return f.reqs[i]
		}
	}
	return stripeReq{}
}

func subJSON(id, customer, status, lookupKey string) string {
	return fmt.Sprintf(`{"id":%q,"customer":%q,"status":%q,"currency":"inr","cancel_at_period_end":true,
		"items":{"data":[{"current_period_end":1900000000,"price":{"id":"price_x","lookup_key":%q}}]}}`, id, customer, status, lookupKey)
}

// setup makes Ann's workspace with Bob as a plain member, and a billing handler against a fake Stripe whose config
// is cfg (stripe_secret_key, stripe_webhook_secret and app_base_url set).
func setup(t *testing.T) (pool *pgxpool.Pool, h *Handler, mux *http.ServeMux, f *fakeStripe, cfg map[string]string, wsID, slug, ann, bob string) {
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
	f = &fakeStripe{subs: map[string]string{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	cfg = map[string]string{"stripe_secret_key": "rk_test", "stripe_webhook_secret": testSecret, "app_base_url": "http://app.test"}
	h = New(pool, sysconfig.New(pool), dbtest.Auth, workspace.New(pool, dbtest.Auth, nil, sysconfig.New(pool)).Member)
	h.stripe.base = srv.URL
	h.config = func(_ context.Context, key string) (string, error) { return cfg[key], nil }
	mux = http.NewServeMux()
	h.Register(mux)
	return
}

// TestWebhookSync: a signed subscription event sets the plan from the subscription Stripe returns (status, currency,
// renews_at from the item, an audit entry with no actor), a bad signature is refused, a canceled subscription returns
// the workspace to Starter, a comped workspace keeps its plan, and a refund and a dispute are audited.
func TestWebhookSync(t *testing.T) {
	pool, _, mux, f, _, wsID, _, _, _ := setup(t)
	ctx := context.Background()
	cus := dbtest.Name("cus_")
	if _, err := pool.Exec(ctx, `update workspaces set stripe_customer_id = $2 where id = $1`, wsID, cus); err != nil {
		t.Fatal(err)
	}
	send := func(typ, object string, want int) {
		t.Helper()
		body := fmt.Sprintf(`{"id":"evt_%d","type":%q,"data":{"object":%s}}`, time.Now().UnixNano(), typ, object)
		r := httptest.NewRequest("POST", "/api/billing/webhook", strings.NewReader(body))
		r.Header.Set("Stripe-Signature", sign(testSecret, time.Now().Unix(), []byte(body)))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s: %d %s, want %d", typ, w.Code, w.Body, want)
		}
	}
	type state struct {
		plan             string
		comped           bool
		sub, status, cur *string
		renews           *time.Time
		cancel           bool
	}
	read := func() state {
		t.Helper()
		var s state
		if err := pool.QueryRow(ctx, `select plan, plan_comped, stripe_subscription_id, subscription_status, billing_currency,
			renews_at, cancel_at_period_end from workspaces where id = $1`, wsID).
			Scan(&s.plan, &s.comped, &s.sub, &s.status, &s.cur, &s.renews, &s.cancel); err != nil {
			t.Fatal(err)
		}
		return s
	}

	f.subs["sub_1"] = subJSON("sub_1", cus, "active", "pro_monthly")
	send("customer.subscription.created", `{"id":"sub_1"}`, 200)
	s := read()
	if s.plan != "pro" || s.sub == nil || *s.sub != "sub_1" || *s.status != "active" || *s.cur != "inr" || !s.cancel ||
		s.renews == nil || s.renews.Unix() != 1900000000 {
		t.Fatalf("after created: %+v", s)
	}
	var detail string
	var actor *string
	if err := pool.QueryRow(ctx, `select detail::text, actor_id::text from workspace_audit where workspace_id = $1 and action = 'billing.plan_changed'`, wsID).
		Scan(&detail, &actor); err != nil || actor != nil || detail != `{"to": "pro", "from": "starter"}` {
		t.Fatalf("audit %s %v %v", detail, actor, err)
	}

	r := httptest.NewRequest("POST", "/api/billing/webhook", strings.NewReader(`{"type":"customer.subscription.deleted"}`))
	r.Header.Set("Stripe-Signature", sign("whsec_wrong", time.Now().Unix(), []byte(`{"type":"customer.subscription.deleted"}`)))
	w := httptest.NewRecorder()
	if mux.ServeHTTP(w, r); w.Code != 400 {
		t.Fatalf("bad signature: %d", w.Code)
	}

	send("invoice.paid", `{"id":"in_1","parent":{"subscription_details":{"subscription":"sub_1"}}}`, 200)
	if s := read(); s.plan != "pro" {
		t.Fatalf("after invoice.paid: %+v", s)
	}

	f.subs["sub_1"] = subJSON("sub_1", cus, "canceled", "pro_monthly")
	send("customer.subscription.deleted", `{"id":"sub_1"}`, 200)
	if s := read(); s.plan != "starter" || s.sub != nil || s.status != nil || s.renews != nil || s.cancel || *s.cur != "inr" {
		t.Fatalf("after deleted: %+v", s)
	}

	if _, err := pool.Exec(ctx, `update workspaces set plan = 'ultimate', plan_comped = true where id = $1`, wsID); err != nil {
		t.Fatal(err)
	}
	f.subs["sub_2"] = subJSON("sub_2", cus, "active", "pro_monthly")
	send("checkout.session.completed", fmt.Sprintf(`{"id":"cs_1","mode":"subscription","client_reference_id":%q,"customer":%q,"subscription":"sub_2"}`, wsID, cus), 200)
	if s := read(); s.plan != "ultimate" || !s.comped || s.sub == nil || *s.sub != "sub_2" {
		t.Fatalf("comped after checkout: %+v", s)
	}

	send("charge.refunded", fmt.Sprintf(`{"id":"ch_1","customer":%q,"amount_refunded":2900,"currency":"usd"}`, cus), 200)
	send("radar.early_fraud_warning.created", `{"id":"issfr_1"}`, 200)
	var n int
	if err := pool.QueryRow(ctx, `select count(*) from workspace_audit where workspace_id = $1 and action = 'billing.refunded'
		and detail->>'amount' = '2900'`, wsID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("refunds audited %d %v", n, err)
	}
	f.chargeCustomer = cus
	send("charge.dispute.created", `{"id":"dp_1","charge":"ch_1","amount":2900,"currency":"usd","reason":"fraudulent"}`, 200)
	if err := pool.QueryRow(ctx, `select count(*) from workspace_audit where workspace_id = $1 and action = 'billing.disputed'
		and detail->>'reason' = 'fraudulent'`, wsID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("disputes audited %d %v", n, err)
	}
}

// TestCheckout: only billing.manage may start a checkout, for a paid plan, while billing is on and there's no
// subscription; the first creates the customer, and the session is in the caller's currency with the plan's price
// and the workspace as client_reference_id. Also the plan list's prices, the billing state and the portal.
func TestCheckout(t *testing.T) {
	pool, _, mux, f, cfg, wsID, slug, ann, bob := setup(t)
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
	pro := url.Values{"plan": {"pro"}}

	do(bob, "POST", base+"/checkout", pro, 403)
	do(ann, "POST", base+"/checkout", url.Values{"plan": {"starter"}}, 400)
	do(ann, "POST", base+"/portal", nil, 409)
	cfg["stripe_secret_key"] = ""
	do(ann, "POST", base+"/checkout", pro, 503)
	if b := do(ann, "GET", base, nil, 200); b["enabled"] != false {
		t.Fatalf("billing off: %v", b)
	}
	cfg["stripe_secret_key"] = "rk_test"

	if got := do(ann, "POST", base+"/checkout", pro, 200)["url"]; got != "https://checkout.stripe.test/cs_1" {
		t.Fatalf("checkout url %v", got)
	}
	c := f.last("/v1/customers")
	if c.form.Get("name") == "" || c.form.Get("metadata[workspace_id]") != wsID ||
		!strings.HasSuffix(c.form.Get("email"), "@dbtest.test") || c.idem != "customer-"+wsID {
		t.Fatalf("customer %+v", c)
	}
	s := f.last("/v1/checkout/sessions").form
	back := "http://app.test/" + slug + "/settings/billing"
	for k, v := range map[string]string{"mode": "subscription", "customer": "cus_new", "line_items[0][price]": "price_pro",
		"line_items[0][quantity]": "1", "currency": "inr", "client_reference_id": wsID, "allow_promotion_codes": "true",
		"success_url": back + "?checkout=done", "cancel_url": back} {
		if s.Get(k) != v {
			t.Errorf("checkout %s = %q, want %q", k, s.Get(k), v)
		}
	}
	if !strings.HasPrefix(s.Get("integration_identifier"), "taskwell_billing_") || s.Has("payment_method_types[0]") || s.Has("payment_method_types[]") {
		t.Errorf("checkout form %v", s)
	}
	var stored string
	if err := pool.QueryRow(ctx, `select stripe_customer_id from workspaces where id = $1`, wsID).Scan(&stored); err != nil || stored != "cus_new" {
		t.Fatalf("customer stored %q %v", stored, err)
	}

	plans := do("", "GET", "/api/billing/plans", nil, 200)["plans"].([]any)
	if p := plans[1].(map[string]any); p["id"] != "pro" || p["members"] != 10.0 ||
		fmt.Sprint(p["prices"]) != "map[inr:149900 usd:2900]" {
		t.Fatalf("pro plan %v", p)
	}
	if p := plans[2].(map[string]any); p["members"] != nil || p["storage_bytes"] != nil {
		t.Fatalf("ultimate plan %v", p)
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

	if got := do(ann, "POST", base+"/portal", nil, 200)["url"]; got != "https://billing.stripe.test/p/1" ||
		f.last("/v1/billing_portal/sessions").form.Get("return_url") != back {
		t.Fatalf("portal %v %+v", got, f.last("/v1/billing_portal/sessions"))
	}
	if _, err := pool.Exec(ctx, `update workspaces set stripe_subscription_id = 'sub_x', subscription_status = 'active' where id = $1`, wsID); err != nil {
		t.Fatal(err)
	}
	do(ann, "POST", base+"/checkout", pro, 409)
}
