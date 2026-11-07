package billing

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/plan"
	"github.com/devender07/taskwell/backend/user/internal/sysconfig"
	"github.com/devender07/taskwell/backend/user/internal/web"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

const (
	errNotFound       = "Workspace not found."
	errForbidden      = "You don't have permission to manage billing."
	errOff            = "Billing isn't set up yet."
	errChoosePlan     = "Choose a plan."
	errSubscribed     = "This workspace already has a subscription. Manage it to change plans."
	errNoSubscription = "This workspace has no subscription yet."

	// integrationID tags this app's Checkout Sessions in the Stripe Dashboard.
	integrationID = "taskwell_billing_qmzvktra"
	priceTTL      = time.Hour
	priceRetry    = time.Minute // how long a failed price read is kept, so a Stripe outage isn't hit per request
)

var errNoKey = errors.New("stripe_secret_key is empty")

type Handler struct {
	model        *model
	stripe       *stripeAPI
	config       func(ctx context.Context, key string) (string, error) // sysconfig's Get
	authenticate func(*http.Request) (string, error)
	member       func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)
	now          func() time.Time

	mu    sync.Mutex // guards cache, and is held while it's refilled
	cache priceCache
}

// priceCache is the paid plans' Stripe prices by plan id, as read with key at at.
type priceCache struct {
	key    string
	at     time.Time
	ttl    time.Duration
	prices map[string]price
	err    error
}

// New takes the auth handler's Authenticate and the workspace handler's Member.
func New(db *pgxpool.Pool, cfg *sysconfig.Store, authenticate func(*http.Request) (string, error),
	member func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)) *Handler {
	return &Handler{model: &model{db: db}, stripe: &stripeAPI{base: stripeBase, client: &http.Client{Timeout: 10 * time.Second}},
		config: cfg.Get, authenticate: authenticate, member: member, now: time.Now}
}

// key returns the Stripe secret key, or errNoKey while billing is off.
func (h *Handler) key(ctx context.Context) (string, error) {
	k, err := h.config(ctx, "stripe_secret_key")
	if err == nil && k == "" {
		err = errNoKey
	}
	return k, err
}

// prices returns the paid plans' prices by plan id, read from Stripe by lookup key and kept for priceTTL (a failure
// for priceRetry); errNoKey while billing is off.
func (h *Handler) prices(ctx context.Context) (map[string]price, error) {
	key, err := h.key(ctx)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if c := h.cache; c.key == key && h.now().Sub(c.at) < c.ttl {
		return c.prices, c.err
	}
	form := url.Values{"active": {"true"}, "expand[]": {"data.currency_options"}}
	for _, p := range plan.Plans {
		if p.LookupKey != "" {
			form.Add("lookup_keys[]", p.LookupKey)
		}
	}
	var list struct {
		Data []price `json:"data"`
	}
	ps := map[string]price{}
	err = h.stripe.call(ctx, key, http.MethodGet, "/v1/prices", form, "", &list)
	for _, pr := range list.Data {
		if p, ok := plan.ByLookupKey(pr.LookupKey); ok {
			ps[p.ID] = pr
		}
	}
	if err == nil && len(ps) < len(plan.Plans)-1 {
		err = fmt.Errorf("stripe prices: found %d of %d lookup keys", len(ps), len(plan.Plans)-1)
	}
	ttl := priceTTL
	if err != nil {
		slog.WarnContext(ctx, "stripe prices unavailable", "err", err)
		ps, ttl = nil, priceRetry
	}
	h.cache = priceCache{key: key, at: h.now(), ttl: ttl, prices: ps, err: err}
	return ps, err
}

// amount is the price in currency, in its smallest unit.
func amount(pr price, currency string) (int64, bool) {
	if pr.Currency == currency {
		return pr.UnitAmount, true
	}
	o, ok := pr.CurrencyOptions[currency]
	return o.UnitAmount, ok
}

// limit is n, or nil for 0 (unlimited).
func limit[T int | int64](n T) *T {
	if n == 0 {
		return nil
	}
	return &n
}

// plans lists the plans with their limits and prices (null for a paid plan while billing is off or Stripe can't be
// reached).
func (h *Handler) plans(w http.ResponseWriter, r *http.Request) error {
	ps, _ := h.prices(r.Context()) // logged when read; the paid plans show no price meanwhile
	type planJSON struct {
		ID           string           `json:"id"`
		Name         string           `json:"name"`
		Members      *int             `json:"members"`
		StorageBytes *int64           `json:"storage_bytes"`
		Prices       map[string]int64 `json:"prices"`
	}
	out := make([]planJSON, 0, len(plan.Plans))
	for _, p := range plan.Plans {
		j := planJSON{ID: p.ID, Name: p.Name, Members: limit(p.Members), StorageBytes: limit(p.StorageBytes)}
		if p.LookupKey == "" {
			j.Prices = map[string]int64{"usd": 0, "inr": 0}
		} else if pr, ok := ps[p.ID]; ok {
			usd, okUSD := amount(pr, "usd")
			inr, okINR := amount(pr, "inr")
			if okUSD && okINR {
				j.Prices = map[string]int64{"usd": usd, "inr": inr}
			}
		}
		out = append(out, j)
	}
	web.JSON(w, http.StatusOK, map[string]any{"plans": out})
	return nil
}

// access returns the path's workspace for a member (not a guest), and with manage only for one with billing.manage;
// otherwise it writes the 404 or 403 and returns nil.
func (h *Handler) access(w http.ResponseWriter, r *http.Request, userID string, manage bool) (*workspace.Workspace, error) {
	ws, err := h.member(r.Context(), userID, r.PathValue("slug"))
	switch {
	case err != nil:
		return nil, err
	case ws == nil:
		web.Err(w, http.StatusNotFound, errNotFound)
		return nil, nil
	case workspace.DenyGuest(w, ws):
		return nil, nil
	case manage && !workspace.Has(ws.Permissions, workspace.PermBillingManage):
		web.Err(w, http.StatusForbidden, errForbidden)
		return nil, nil
	}
	return ws, nil
}

// currency is the workspace's billing currency, or for one that has none, INR when the user's timezone is India's:
// their profile's, else browserTZ (the request's "tz", what the browser reports).
// ponytail: a timezone is easy to fake for the INR price; check the card's country at checkout if that gets abused.
func (h *Handler) currency(ctx context.Context, a account, userID, browserTZ string) (string, error) {
	if a.Currency != nil {
		return *a.Currency, nil
	}
	_, tz, err := h.model.caller(ctx, userID)
	if tz == "" {
		tz = browserTZ
	}
	if tz == "Asia/Kolkata" || tz == "Asia/Calcutta" {
		return "inr", err
	}
	return "usd", err
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request, userID string) error {
	ctx := r.Context()
	ws, err := h.access(w, r, userID, false)
	if ws == nil {
		return err
	}
	a, err := h.model.account(ctx, ws.ID)
	if err != nil {
		return err
	}
	seats, bytes, err := h.model.usage(ctx, ws.ID)
	if err != nil {
		return err
	}
	currency, err := h.currency(ctx, a, userID, r.FormValue("tz"))
	if err != nil {
		return err
	}
	_, err = h.key(ctx)
	if err != nil && !errors.Is(err, errNoKey) {
		return err
	}
	p, _ := plan.Get(a.Plan)
	web.JSON(w, http.StatusOK, map[string]any{
		"plan": a.Plan, "comped": a.Comped, "status": a.Status, "currency": currency, "renews_at": a.RenewsAt,
		"cancel_at_period_end": a.CancelAtPeriodEnd,
		"members":              map[string]any{"used": seats, "limit": limit(p.Members)},
		"storage":              map[string]any{"used_bytes": bytes, "limit_bytes": limit(p.StorageBytes)},
		"can_manage":           workspace.Has(ws.Permissions, workspace.PermBillingManage),
		"enabled":              err == nil,
	})
	return nil
}

// back is the billing settings page of the workspace.
func (h *Handler) back(ctx context.Context, slug string) (string, error) {
	base, err := h.config(ctx, "app_base_url")
	return base + "/" + url.PathEscape(slug) + "/settings/billing", err
}

// checkout answers a Stripe Checkout link that subscribes the workspace to the form's plan, creating its Stripe
// customer the first time.
func (h *Handler) checkout(w http.ResponseWriter, r *http.Request, userID string) error {
	ctx := r.Context()
	ws, err := h.access(w, r, userID, true)
	if ws == nil {
		return err
	}
	p, ok := plan.Get(r.FormValue("plan"))
	if !ok || p.LookupKey == "" {
		web.Err(w, http.StatusBadRequest, errChoosePlan)
		return nil
	}
	key, err := h.key(ctx)
	if errors.Is(err, errNoKey) {
		web.Err(w, http.StatusServiceUnavailable, errOff)
		return nil
	}
	if err != nil {
		return err
	}
	a, err := h.model.account(ctx, ws.ID)
	if err != nil {
		return err
	}
	if a.Comped || a.SubscriptionID != nil {
		web.Err(w, http.StatusConflict, errSubscribed)
		return nil
	}
	ps, err := h.prices(ctx)
	if err != nil {
		return err
	}
	currency, err := h.currency(ctx, a, userID, r.FormValue("tz"))
	if err != nil {
		return err
	}
	if _, ok := amount(ps[p.ID], currency); !ok {
		return fmt.Errorf("checkout: price %s has no %s amount", ps[p.ID].ID, currency)
	}
	customer := ""
	if a.CustomerID != nil {
		customer = *a.CustomerID
	} else {
		email, _, err := h.model.caller(ctx, userID)
		if err != nil {
			return err
		}
		var c struct {
			ID string `json:"id"`
		}
		if err := h.stripe.call(ctx, key, http.MethodPost, "/v1/customers",
			url.Values{"name": {ws.Name}, "email": {email}, "metadata[workspace_id]": {ws.ID}}, "customer-"+ws.ID, &c); err != nil {
			return err
		}
		if customer, err = h.model.saveCustomer(ctx, ws.ID, c.ID); err != nil {
			return err
		}
	}
	back, err := h.back(ctx, ws.Slug)
	if err != nil {
		return err
	}
	var s struct {
		URL string `json:"url"`
	}
	if err := h.stripe.call(ctx, key, http.MethodPost, "/v1/checkout/sessions", url.Values{
		"mode":                    {"subscription"},
		"customer":                {customer},
		"line_items[0][price]":    {ps[p.ID].ID},
		"line_items[0][quantity]": {"1"},
		"currency":                {currency},
		"client_reference_id":     {ws.ID},
		"allow_promotion_codes":   {"true"},
		"integration_identifier":  {integrationID},
		"success_url":             {back + "?checkout=done"},
		"cancel_url":              {back},
	}, "", &s); err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]string{"url": s.URL})
	return nil
}

// portal answers a Stripe customer portal link for the workspace, back to its billing page.
func (h *Handler) portal(w http.ResponseWriter, r *http.Request, userID string) error {
	ctx := r.Context()
	ws, err := h.access(w, r, userID, true)
	if ws == nil {
		return err
	}
	key, err := h.key(ctx)
	if errors.Is(err, errNoKey) {
		web.Err(w, http.StatusServiceUnavailable, errOff)
		return nil
	}
	if err != nil {
		return err
	}
	a, err := h.model.account(ctx, ws.ID)
	if err != nil {
		return err
	}
	if a.CustomerID == nil {
		web.Err(w, http.StatusConflict, errNoSubscription)
		return nil
	}
	back, err := h.back(ctx, ws.Slug)
	if err != nil {
		return err
	}
	var s struct {
		URL string `json:"url"`
	}
	if err := h.stripe.call(ctx, key, http.MethodPost, "/v1/billing_portal/sessions",
		url.Values{"customer": {*a.CustomerID}, "return_url": {back}}, "", &s); err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]string{"url": s.URL})
	return nil
}

// webhook takes Stripe's events: a bad signature is a 400, a failure to apply one a 500 (Stripe retries), anything
// else a 200.
func (h *Handler) webhook(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		web.Err(w, http.StatusBadRequest, "Invalid payload.")
		return nil
	}
	secret, err := h.config(ctx, "stripe_webhook_secret")
	if err != nil {
		return err
	}
	var e event
	if !verifySignature(r.Header.Get("Stripe-Signature"), body, secret, h.now()) || json.Unmarshal(body, &e) != nil {
		web.Err(w, http.StatusBadRequest, "Invalid signature.")
		return nil
	}
	if err := h.handle(ctx, e); err != nil {
		return fmt.Errorf("stripe event %s %s: %w", e.Type, e.ID, err)
	}
	web.JSON(w, http.StatusOK, map[string]bool{"received": true})
	return nil
}

// handle applies one event; see docs/billing-plan.md for which.
func (h *Handler) handle(ctx context.Context, e event) error {
	// The fields of every object handled: a checkout session, subscription, invoice, charge or dispute.
	var o struct {
		ID                string `json:"id"`
		Mode              string `json:"mode"`
		ClientReferenceID string `json:"client_reference_id"`
		Customer          string `json:"customer"`
		Subscription      string `json:"subscription"` // a checkout session's; an invoice's on old API versions
		Parent            struct {
			SubscriptionDetails struct {
				Subscription string `json:"subscription"`
			} `json:"subscription_details"`
		} `json:"parent"`
		Charge         string `json:"charge"`
		Amount         int64  `json:"amount"`
		AmountRefunded int64  `json:"amount_refunded"`
		Currency       string `json:"currency"`
		Reason         string `json:"reason"`
	}
	switch e.Type {
	case "checkout.session.completed", "customer.subscription.created", "customer.subscription.updated",
		"customer.subscription.deleted", "invoice.paid", "invoice.payment_failed", "charge.dispute.created", "charge.refunded":
	default:
		return nil
	}
	if err := json.Unmarshal(e.Data.Object, &o); err != nil {
		return fmt.Errorf("decode object: %w", err)
	}
	switch e.Type {
	case "checkout.session.completed":
		if o.Mode != "subscription" || o.Subscription == "" {
			return nil
		}
		if !web.IsUUID(o.ClientReferenceID) {
			slog.WarnContext(ctx, "stripe checkout without a workspace", "event", e.ID, "client_reference_id", o.ClientReferenceID)
			return nil
		}
		found, err := h.model.linkCheckout(ctx, o.ClientReferenceID, o.Customer, o.Subscription)
		if err != nil || !found {
			if !found {
				slog.WarnContext(ctx, "stripe checkout for a missing workspace", "event", e.ID, "workspace", o.ClientReferenceID)
			}
			return err
		}
		return h.syncSubscription(ctx, o.Subscription)
	case "customer.subscription.created", "customer.subscription.updated", "customer.subscription.deleted":
		return h.syncSubscription(ctx, o.ID)
	case "invoice.paid", "invoice.payment_failed":
		if sub := cmp.Or(o.Parent.SubscriptionDetails.Subscription, o.Subscription); sub != "" {
			return h.syncSubscription(ctx, sub)
		}
		return nil
	case "charge.dispute.created":
		key, err := h.key(ctx)
		if err != nil {
			return err
		}
		var c struct {
			Customer string `json:"customer"`
		}
		if err := h.stripe.call(ctx, key, http.MethodGet, "/v1/charges/"+url.PathEscape(o.Charge), nil, "", &c); err != nil {
			return err
		}
		slog.WarnContext(ctx, "stripe dispute", "event", e.ID, "customer", c.Customer, "amount", o.Amount, "currency", o.Currency, "reason", o.Reason)
		return h.recordCharge(ctx, e, c.Customer, "billing.disputed", map[string]any{"amount": o.Amount, "currency": o.Currency, "reason": o.Reason})
	default: // charge.refunded
		slog.WarnContext(ctx, "stripe refund", "event", e.ID, "customer", o.Customer, "amount", o.AmountRefunded, "currency", o.Currency)
		return h.recordCharge(ctx, e, o.Customer, "billing.refunded", map[string]any{"amount": o.AmountRefunded, "currency": o.Currency})
	}
}

func (h *Handler) recordCharge(ctx context.Context, e event, customer, action string, detail map[string]any) error {
	found, err := h.model.recordCharge(ctx, customer, action, detail)
	if err == nil && !found {
		slog.WarnContext(ctx, "stripe charge for no workspace", "event", e.ID, "customer", customer)
	}
	return err
}

// syncSubscription reads the subscription from Stripe (events arrive out of order: the current object wins) and
// applies it to its workspace.
func (h *Handler) syncSubscription(ctx context.Context, id string) error {
	key, err := h.key(ctx)
	if err != nil {
		return err
	}
	var s subscription
	if err := h.stripe.call(ctx, key, http.MethodGet, "/v1/subscriptions/"+url.PathEscape(id), nil, "", &s); err != nil {
		return err
	}
	found, err := h.model.sync(ctx, s)
	if err == nil && !found {
		slog.WarnContext(ctx, "stripe subscription for no workspace", "subscription", id, "customer", s.Customer)
	}
	return err
}
