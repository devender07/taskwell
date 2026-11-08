package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
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

	priceTTL   = time.Hour
	priceRetry = time.Minute // how long a failed price read is kept, so a Paddle outage isn't hit per request
)

var errBillingOff = errors.New("billing is off: a paddle_ key is empty")

// keeps are the subscription statuses that keep the paid plan.
var keeps = []string{"active", "trialing", "past_due"}

type Handler struct {
	model        *model
	paddle       *paddleAPI
	config       func(ctx context.Context, key string) (string, error) // sysconfig's Get
	authenticate func(*http.Request) (string, error)
	member       func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)
	now          func() time.Time

	mu    sync.Mutex // guards cache, and is held while it's refilled
	cache priceCache
}

// priceCache is the paid plans' Paddle prices by plan id, as read with settings s at at.
type priceCache struct {
	s      settings
	at     time.Time
	ttl    time.Duration
	prices map[string]price
	err    error
}

// New takes the auth handler's Authenticate and the workspace handler's Member.
func New(db *pgxpool.Pool, cfg *sysconfig.Store, authenticate func(*http.Request) (string, error),
	member func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)) *Handler {
	return &Handler{model: &model{db: db}, paddle: &paddleAPI{client: &http.Client{Timeout: 10 * time.Second}},
		config: cfg.Get, authenticate: authenticate, member: member, now: time.Now}
}

// settings is billing's paddle_ config; prices are the price ids by plan id.
type settings struct {
	env, key, token string
	prices          map[string]string
}

// planOf is the paid plan whose price id is id.
func (s settings) planOf(id string) (string, bool) {
	for p, pr := range s.prices {
		if pr == id {
			return p, true
		}
	}
	return "", false
}

// settings reads the paddle_ config, or errBillingOff while billing is off (the key, client token or a price id empty).
func (h *Handler) settings(ctx context.Context) (settings, error) {
	get := func(key string, err error) (string, error) {
		if err != nil {
			return "", err
		}
		return h.config(ctx, key)
	}
	var s settings
	var pro, ult string
	var err error
	s.env, err = get("paddle_environment", err)
	s.key, err = get("paddle_api_key", err)
	s.token, err = get("paddle_client_token", err)
	pro, err = get("paddle_price_pro", err)
	ult, err = get("paddle_price_ultimate", err)
	if err != nil {
		return s, err
	}
	s.prices = map[string]string{"pro": pro, "ultimate": ult}
	if s.key == "" || s.token == "" || pro == "" || ult == "" {
		return s, errBillingOff
	}
	return s, nil
}

// on is settings for a handler that needs billing on: false after writing the 503 while it's off.
func (h *Handler) on(ctx context.Context, w http.ResponseWriter) (settings, bool, error) {
	s, err := h.settings(ctx)
	if errors.Is(err, errBillingOff) {
		web.Err(w, http.StatusServiceUnavailable, errOff)
		return s, false, nil
	}
	return s, err == nil, err
}

// prices returns the paid plans' prices by plan id, read from Paddle and kept for priceTTL (a failure for
// priceRetry); errBillingOff while billing is off.
func (h *Handler) prices(ctx context.Context) (map[string]price, error) {
	s, err := h.settings(ctx)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if c := h.cache; c.s.key == s.key && c.s.env == s.env && c.s.prices["pro"] == s.prices["pro"] &&
		c.s.prices["ultimate"] == s.prices["ultimate"] && h.now().Sub(c.at) < c.ttl {
		return c.prices, c.err
	}
	ps := map[string]price{}
	for p, id := range s.prices {
		var pr price
		if err = h.paddle.call(ctx, s.key, s.env, http.MethodGet, "/prices/"+url.PathEscape(id), nil, &pr); err != nil {
			break
		}
		ps[p] = pr
	}
	ttl := priceTTL
	if err != nil {
		slog.WarnContext(ctx, "paddle prices unavailable", "err", err)
		ps, ttl = nil, priceRetry
	}
	h.cache = priceCache{s: s, at: h.now(), ttl: ttl, prices: ps, err: err}
	return ps, err
}

// limit is n, or nil for 0 (unlimited).
func limit[T int | int64](n T) *T {
	if n == 0 {
		return nil
	}
	return &n
}

// plans lists the plans with their limits and prices (null for a paid plan while billing is off or Paddle can't be
// reached).
func (h *Handler) plans(w http.ResponseWriter, r *http.Request) error {
	ps, _ := h.prices(r.Context()) // logged when read; the paid plans show no price meanwhile
	type planJSON struct {
		ID           string            `json:"id"`
		Name         string            `json:"name"`
		Members      *int              `json:"members"`
		StorageBytes *int64            `json:"storage_bytes"`
		Prices       map[string]*int64 `json:"prices"`
	}
	out := make([]planJSON, 0, len(plan.Plans))
	for _, p := range plan.Plans {
		j := planJSON{ID: p.ID, Name: p.Name, Members: limit(p.Members), StorageBytes: limit(p.StorageBytes),
			Prices: map[string]*int64{"usd": nil, "inr": nil}}
		if p.ID == plan.Starter {
			var zero int64
			j.Prices = map[string]*int64{"usd": &zero, "inr": &zero}
		} else if pr, ok := ps[p.ID]; ok {
			if usd, inr, ok := pr.amounts(); ok {
				j.Prices = map[string]*int64{"usd": &usd, "inr": &inr}
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
// their profile's, else browserTZ (the request's "tz", what the browser reports). Only for display: Paddle charges
// in the currency of the billing address.
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
	ws, err := h.access(w, r, userID, false)
	if ws == nil {
		return err
	}
	return h.state(w, r, ws, userID)
}

// state answers the workspace's billing state.
func (h *Handler) state(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace, userID string) error {
	ctx := r.Context()
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
	_, err = h.settings(ctx)
	if err != nil && !errors.Is(err, errBillingOff) {
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

// paidPlan is the form's plan when it's a paid one; otherwise it writes the 400.
func paidPlan(w http.ResponseWriter, r *http.Request) (string, bool) {
	p := r.FormValue("plan")
	if p != "pro" && p != "ultimate" {
		web.Err(w, http.StatusBadRequest, errChoosePlan)
		return "", false
	}
	return p, true
}

// item is a subscription or transaction line: one of the price.
type item struct {
	PriceID  string `json:"price_id"`
	Quantity int    `json:"quantity"`
}

// checkout makes a Paddle transaction for the form's plan and answers what Paddle.js needs to open its checkout.
func (h *Handler) checkout(w http.ResponseWriter, r *http.Request, userID string) error {
	ctx := r.Context()
	ws, err := h.access(w, r, userID, true)
	if ws == nil {
		return err
	}
	p, ok := paidPlan(w, r)
	if !ok {
		return nil
	}
	s, ok, err := h.on(ctx, w)
	if !ok {
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
	email, _, err := h.model.caller(ctx, userID)
	if err != nil {
		return err
	}
	body := struct {
		Items      []item            `json:"items"`
		CustomData map[string]string `json:"custom_data"`
		CustomerID *string           `json:"customer_id,omitempty"`
	}{[]item{{s.prices[p], 1}}, map[string]string{"workspace_id": ws.ID}, a.CustomerID}
	var t struct {
		ID string `json:"id"`
	}
	if err := h.paddle.call(ctx, s.key, s.env, http.MethodPost, "/transactions", body, &t); err != nil {
		return err
	}
	env := "sandbox"
	if s.env == "production" {
		env = "production"
	}
	web.JSON(w, http.StatusOK, map[string]string{"transaction_id": t.ID, "client_token": s.token, "environment": env, "email": email})
	return nil
}

// change moves the workspace's subscription to the form's plan, charging the prorated difference now, and answers
// the billing state.
func (h *Handler) change(w http.ResponseWriter, r *http.Request, userID string) error {
	ctx := r.Context()
	ws, err := h.access(w, r, userID, true)
	if ws == nil {
		return err
	}
	p, ok := paidPlan(w, r)
	if !ok {
		return nil
	}
	s, ok, err := h.on(ctx, w)
	if !ok {
		return err
	}
	a, err := h.model.account(ctx, ws.ID)
	if err != nil {
		return err
	}
	if a.SubscriptionID == nil {
		web.Err(w, http.StatusConflict, errNoSubscription)
		return nil
	}
	if a.Plan == p {
		web.Err(w, http.StatusBadRequest, errChoosePlan)
		return nil
	}
	body := map[string]any{"items": []item{{s.prices[p], 1}}, "proration_billing_mode": "prorated_immediately"}
	var sub subscription
	if err := h.paddle.call(ctx, s.key, s.env, http.MethodPatch, "/subscriptions/"+url.PathEscape(*a.SubscriptionID), body, &sub); err != nil {
		return err
	}
	if err := h.apply(ctx, s, sub); err != nil {
		return err
	}
	return h.state(w, r, ws, userID)
}

// portal answers a Paddle customer portal link for the workspace's subscription.
func (h *Handler) portal(w http.ResponseWriter, r *http.Request, userID string) error {
	ctx := r.Context()
	ws, err := h.access(w, r, userID, true)
	if ws == nil {
		return err
	}
	s, ok, err := h.on(ctx, w)
	if !ok {
		return err
	}
	a, err := h.model.account(ctx, ws.ID)
	if err != nil {
		return err
	}
	if a.CustomerID == nil || a.SubscriptionID == nil {
		web.Err(w, http.StatusConflict, errNoSubscription)
		return nil
	}
	var ps struct {
		URLs struct {
			General struct {
				Overview string `json:"overview"`
			} `json:"general"`
		} `json:"urls"`
	}
	if err := h.paddle.call(ctx, s.key, s.env, http.MethodPost, "/customers/"+url.PathEscape(*a.CustomerID)+"/portal-sessions",
		map[string][]string{"subscription_ids": {*a.SubscriptionID}}, &ps); err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]string{"url": ps.URLs.General.Overview})
	return nil
}

// webhook takes Paddle's notifications: a bad signature is a 400, a failure to apply one a 500 (Paddle retries),
// anything else a 200.
func (h *Handler) webhook(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		web.Err(w, http.StatusBadRequest, "Invalid payload.")
		return nil
	}
	secret, err := h.config(ctx, "paddle_webhook_secret")
	if err != nil {
		return err
	}
	var e event
	if !verifySignature(r.Header.Get("Paddle-Signature"), body, secret, h.now()) || json.Unmarshal(body, &e) != nil {
		web.Err(w, http.StatusBadRequest, "Invalid signature.")
		return nil
	}
	if err := h.handle(ctx, e); err != nil {
		return fmt.Errorf("paddle event %s %s: %w", e.Type, e.ID, err)
	}
	web.JSON(w, http.StatusOK, map[string]bool{"received": true})
	return nil
}

// handle applies one event; see docs/billing-plan.md for which.
func (h *Handler) handle(ctx context.Context, e event) error {
	switch e.Type {
	case "subscription.created", "subscription.updated", "subscription.activated", "subscription.past_due",
		"subscription.paused", "subscription.resumed", "subscription.canceled":
		var o subscription
		if err := json.Unmarshal(e.Data, &o); err != nil {
			return fmt.Errorf("decode subscription: %w", err)
		}
		return h.syncSubscription(ctx, o.ID, o.CustomData.WorkspaceID)
	case "adjustment.created":
		var o struct {
			ID             string `json:"id"`
			Action         string `json:"action"`
			SubscriptionID string `json:"subscription_id"`
			CurrencyCode   string `json:"currency_code"`
			Reason         string `json:"reason"`
			Totals         struct {
				Total string `json:"total"`
			} `json:"totals"`
		}
		if err := json.Unmarshal(e.Data, &o); err != nil {
			return fmt.Errorf("decode adjustment: %w", err)
		}
		action := map[string]string{"refund": "billing.refunded", "chargeback": "billing.disputed"}[o.Action]
		if action == "" {
			return nil
		}
		slog.WarnContext(ctx, "paddle "+o.Action, "event", e.ID, "adjustment", o.ID, "subscription", o.SubscriptionID,
			"amount", o.Totals.Total, "currency", o.CurrencyCode, "reason", o.Reason)
		found, err := h.model.recordAdjustment(ctx, o.SubscriptionID, action,
			map[string]any{"amount": o.Totals.Total, "currency": o.CurrencyCode, "reason": o.Reason})
		if err == nil && !found {
			slog.WarnContext(ctx, "paddle adjustment for no workspace", "event", e.ID, "subscription", o.SubscriptionID)
		}
		return err
	}
	return nil
}

// syncSubscription reads the subscription from Paddle (events arrive out of order: the current object wins) and
// applies it to its workspace: the subscription's custom_data workspace, else the event's (workspaceID), else the one
// holding the subscription.
func (h *Handler) syncSubscription(ctx context.Context, id, workspaceID string) error {
	s, err := h.settings(ctx)
	if err != nil {
		return err
	}
	var sub subscription
	if err := h.paddle.call(ctx, s.key, s.env, http.MethodGet, "/subscriptions/"+url.PathEscape(id), nil, &sub); err != nil {
		return err
	}
	if sub.CustomData.WorkspaceID == "" {
		sub.CustomData.WorkspaceID = workspaceID
	}
	return h.apply(ctx, s, sub)
}

// apply stores Paddle's current subscription on its workspace.
func (h *Handler) apply(ctx context.Context, s settings, sub subscription) error {
	paid := ""
	if slices.Contains(keeps, sub.Status) {
		if len(sub.Items) == 0 {
			return fmt.Errorf("sync %s: no items", sub.ID)
		}
		var ok bool
		if paid, ok = s.planOf(sub.Items[0].Price.ID); !ok {
			return fmt.Errorf("sync %s: unknown price %q", sub.ID, sub.Items[0].Price.ID)
		}
	}
	found, err := h.model.sync(ctx, sub, paid)
	if err == nil && !found {
		slog.WarnContext(ctx, "paddle subscription for no workspace", "subscription", sub.ID, "workspace", sub.CustomData.WorkspaceID)
	}
	return err
}
