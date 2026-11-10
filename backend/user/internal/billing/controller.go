package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/mail"
	"github.com/devender07/taskwell/backend/user/internal/plan"
	"github.com/devender07/taskwell/backend/user/internal/secret"
	"github.com/devender07/taskwell/backend/user/internal/sysconfig"
	"github.com/devender07/taskwell/backend/user/internal/web"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

const (
	errNotFound       = "Workspace not found."
	errOff            = "Billing isn't set up yet."
	errChoosePlan     = "Choose a plan."
	errSubscribed     = "You already have a subscription. Manage it to change plans."
	errNoSubscription = "You don't have a subscription yet."
)

var errBillingOff = errors.New("billing is off: paddle_environment isn't sandbox or production, or paddle_api_key is empty")

// granting are the subscription statuses that grant access to its plan: past_due is the grace while Paddle retries.
var granting = []string{"active", "trialing", "past_due"}

// grantsAccess reports whether a subscription with the status grants access to its plan; a scheduled change never
// revokes it by itself.
func grantsAccess(status string) bool { return slices.Contains(granting, status) }

type Handler struct {
	model        *model
	paddle       *paddleAPI
	config       func(ctx context.Context, key string) (string, error) // sysconfig's Get
	key          []byte                                                // signs checkout custom_data
	authenticate func(*http.Request) (string, error)
	member       func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)
	mail         *mail.Mailer
	now          func() time.Time
}

// New takes the JWT secret (the checkout signature's key is derived from it), the auth handler's Authenticate and
// the workspace handler's Member; mailer sends the email when a comp ends (see Run).
func New(db *pgxpool.Pool, cfg *sysconfig.Store, mailer *mail.Mailer, jwtSecret string, authenticate func(*http.Request) (string, error),
	member func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)) *Handler {
	return &Handler{model: &model{db: db}, paddle: &paddleAPI{client: &http.Client{Timeout: 10 * time.Second}},
		config: cfg.Get, key: secret.Key("taskwell checkout", "jwt_secret:"+jwtSecret), authenticate: authenticate,
		member: member, mail: mailer, now: time.Now}
}

// settings is billing's paddle_ config.
type settings struct{ env, key string }

// settings reads the paddle_ config, or errBillingOff while billing is off: paddle_environment isn't exactly
// sandbox or production (logged as an error: it's seeded empty so a deploy sets it on purpose) or the key is empty.
func (h *Handler) settings(ctx context.Context) (settings, error) {
	var s settings
	var err error
	if s.env, err = h.config(ctx, "paddle_environment"); err != nil {
		return s, err
	}
	if s.key, err = h.config(ctx, "paddle_api_key"); err != nil {
		return s, err
	}
	if s.env != "sandbox" && s.env != "production" {
		slog.ErrorContext(ctx, "billing is off: paddle_environment must be sandbox or production", "paddle_environment", s.env)
		return s, errBillingOff
	}
	if s.key == "" {
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

// mac is the HMAC-SHA256 of the user id that checkout custom_data carries (hex, as sig), so a subscription made from
// it can be linked to the account: Paddle.js checkout takes any custom_data from the browser.
func (h *Handler) mac(userID string) []byte {
	m := hmac.New(sha256.New, h.key)
	m.Write([]byte(userID))
	return m.Sum(nil)
}

// signed reports whether c's sig is its user id's.
func (h *Handler) signed(c customData) bool {
	got, err := hex.DecodeString(c.Sig)
	return err == nil && web.IsUUID(c.UserID) && hmac.Equal(got, h.mac(c.UserID))
}

// link is the user id c names when it's signed, else "".
func (h *Handler) link(c customData) string {
	if h.signed(c) {
		return c.UserID
	}
	return ""
}

// limit is n, or nil for 0 (unlimited).
func limit[T int | int64](n T) *T {
	if n == 0 {
		return nil
	}
	return &n
}

// state answers the caller's account billing state.
func (h *Handler) state(w http.ResponseWriter, r *http.Request, userID string) error {
	ctx := r.Context()
	a, err := h.model.account(ctx, userID)
	if err != nil {
		return err
	}
	_, err = h.settings(ctx)
	if err != nil && !errors.Is(err, errBillingOff) {
		return err
	}
	p, _ := plan.Get(a.Plan)
	web.JSON(w, http.StatusOK, map[string]any{
		"plan": a.Plan, "comped": a.Comped, "comped_until": a.CompedUntil, "status": a.Status, "currency": a.Currency, "interval": a.Interval,
		"renews_at": a.RenewsAt, "cancel_at_period_end": a.CancelAtPeriodEnd, "trial_used": a.TrialUsed,
		"workspaces": map[string]any{"used": a.Owned, "limit": limit(p.Workspaces)},
		"enabled":    err == nil,
	})
	return nil
}

// workspace answers the path's workspace's view of its owner's plan, for a member (not a guest).
func (h *Handler) workspace(w http.ResponseWriter, r *http.Request, userID string) error {
	ctx := r.Context()
	ws, err := h.member(ctx, userID, r.PathValue("slug"))
	switch {
	case err != nil:
		return err
	case ws == nil:
		web.Err(w, http.StatusNotFound, errNotFound)
		return nil
	case workspace.DenyGuest(w, ws):
		return nil
	}
	u, err := h.model.usage(ctx, ws.ID)
	if err != nil {
		return err
	}
	var owner map[string]string
	if u.OwnerID != nil {
		owner = map[string]string{"id": *u.OwnerID, "name": *u.OwnerName}
	}
	p, _ := plan.Get(u.Plan)
	web.JSON(w, http.StatusOK, map[string]any{
		"plan": u.Plan, "owner": owner, "is_owner": u.OwnerID != nil && *u.OwnerID == userID,
		"projects": map[string]any{"used": u.Projects, "limit": limit(p.Projects)},
		"members":  map[string]any{"used": u.Seats, "limit": limit(p.Members)},
		"storage":  map[string]any{"used_bytes": u.Bytes, "limit_bytes": limit(p.StorageBytes)},
	})
	return nil
}

// paidPlan is the price's paid plan (its custom_data.taskwell_plan), false when it has none.
func paidPlan(p price) (string, bool) {
	id := p.CustomData.Plan
	_, ok := plan.Get(id)
	return id, ok && id != plan.Free
}

// item is a subscription line: one of the price.
type item struct {
	PriceID  string `json:"price_id"`
	Quantity int    `json:"quantity"`
}

// checkout answers the custom_data, signed, and the email that the pricing page opens a Paddle.js checkout with,
// for an account with no subscription in force that isn't comped. Paddle isn't called: the form's price_id only has
// to be there.
func (h *Handler) checkout(w http.ResponseWriter, r *http.Request, userID string) error {
	ctx := r.Context()
	if r.FormValue("price_id") == "" {
		web.Err(w, http.StatusBadRequest, errChoosePlan)
		return nil
	}
	if _, ok, err := h.on(ctx, w); !ok {
		return err
	}
	a, err := h.model.account(ctx, userID)
	if err != nil {
		return err
	}
	if a.Comped || a.SubscriptionID != nil {
		web.Err(w, http.StatusConflict, errSubscribed)
		return nil
	}
	email, err := h.model.email(ctx, userID)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]any{"custom_data": customData{userID, hex.EncodeToString(h.mac(userID))}, "email": email})
	return nil
}

// change moves the account's subscription to the form's price (another plan or interval), charging the prorated
// difference now (in a trial, nothing until it ends), and answers the billing state.
func (h *Handler) change(w http.ResponseWriter, r *http.Request, userID string) error {
	ctx := r.Context()
	id := r.FormValue("price_id")
	if id == "" {
		web.Err(w, http.StatusBadRequest, errChoosePlan)
		return nil
	}
	s, ok, err := h.on(ctx, w)
	if !ok {
		return err
	}
	a, err := h.model.account(ctx, userID)
	if err != nil {
		return err
	}
	if a.SubscriptionID == nil {
		web.Err(w, http.StatusConflict, errNoSubscription)
		return nil
	}
	var pr price
	var pe *paddleError
	err = h.paddle.call(ctx, s.key, s.env, http.MethodGet, "/prices/"+url.PathEscape(id), nil, &pr)
	if errors.As(err, &pe) && (pe.status == http.StatusNotFound || pe.status == http.StatusBadRequest) {
		web.Err(w, http.StatusBadRequest, errChoosePlan)
		return nil
	}
	if err != nil {
		return err
	}
	if _, ok := paidPlan(pr); !ok || pr.Status != "active" || pr.ID == *a.PriceID {
		web.Err(w, http.StatusBadRequest, errChoosePlan)
		return nil
	}
	// Paddle decides whether it's in a trial (the mirror can lag): a trialing subscription refuses any mode but
	// do_not_bill, and then there's nothing to prorate; the new price is billed when the trial ends.
	path := "/subscriptions/" + url.PathEscape(*a.SubscriptionID)
	var sub subscription
	err = h.paddle.call(ctx, s.key, s.env, http.MethodPatch, path,
		map[string]any{"items": []item{{pr.ID, 1}}, "proration_billing_mode": "prorated_immediately"}, &sub)
	if errors.As(err, &pe) && pe.code == "subscription_trialing_items_update_invalid_options" {
		err = h.paddle.call(ctx, s.key, s.env, http.MethodPatch, path,
			map[string]any{"items": []item{{pr.ID, 1}}, "proration_billing_mode": "do_not_bill"}, &sub)
	}
	if err != nil {
		return err
	}
	if err := h.saveSubscription(ctx, sub); err != nil {
		return err
	}
	return h.state(w, r, userID)
}

// portal answers a Paddle customer portal link for the account's subscription.
func (h *Handler) portal(w http.ResponseWriter, r *http.Request, userID string) error {
	ctx := r.Context()
	s, ok, err := h.on(ctx, w)
	if !ok {
		return err
	}
	customerID, subID, found, err := h.model.portalTarget(ctx, userID)
	if err != nil {
		return err
	}
	if !found {
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
	if err := h.paddle.call(ctx, s.key, s.env, http.MethodPost, "/customers/"+url.PathEscape(customerID)+"/portal-sessions",
		map[string][]string{"subscription_ids": {subID}}, &ps); err != nil {
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

// handle applies one event, from its payload only (docs/billing-plan.md): subscriptions, customers, completed or paid
// transactions and adjustments are mirrored (docs/revenue-plan.md), a completed transaction links its customer,
// anything else is ignored.
func (h *Handler) handle(ctx context.Context, e event) error {
	switch e.Type {
	case "subscription.created", "subscription.updated", "subscription.canceled", "subscription.activated",
		"subscription.trialing", "subscription.past_due", "subscription.paused", "subscription.resumed":
		var o subscription
		if err := json.Unmarshal(e.Data, &o); err != nil {
			return fmt.Errorf("decode subscription: %w", err)
		}
		return h.saveSubscription(ctx, o)
	case "customer.created", "customer.updated":
		var o customer
		if err := json.Unmarshal(e.Data, &o); err != nil {
			return fmt.Errorf("decode customer: %w", err)
		}
		if o.ID == "" || o.UpdatedAt.IsZero() {
			return fmt.Errorf("customer %q: no id or updated_at", o.ID)
		}
		return h.model.saveCustomer(ctx, o)
	case "transaction.completed", "transaction.paid", "transaction.updated":
		var o transaction
		if err := json.Unmarshal(e.Data, &o); err != nil {
			return fmt.Errorf("decode transaction: %w", err)
		}
		if e.Type == "transaction.completed" {
			slog.InfoContext(ctx, "paddle transaction completed", "event", e.ID, "transaction", o.ID, "customer", o.CustomerID,
				"subscription", o.SubscriptionID)
			if o.CustomerID != "" {
				if err := h.model.linkCustomer(ctx, o.CustomerID, h.link(o.CustomData)); err != nil {
					return err
				}
			}
		}
		return h.saveTransaction(ctx, o)
	case "adjustment.created", "adjustment.updated":
		var o adjustment
		if err := json.Unmarshal(e.Data, &o); err != nil {
			return fmt.Errorf("decode adjustment: %w", err)
		}
		slog.WarnContext(ctx, "paddle adjustment "+o.Action, "event", e.ID, "adjustment", o.ID, "transaction", o.TransactionID,
			"status", o.Status, "amount", o.Totals.Total, "currency", o.Totals.CurrencyCode, "reason", o.Reason)
		return h.saveAdjustment(ctx, o)
	}
	return nil
}

// saveSubscription mirrors a subscription entity (see model.saveSubscription), linking a new one to the account its
// custom_data names when that's signed. Its plan is its first item's price's custom_data.taskwell_plan; an unknown
// one on a subscription granting access is an error, so Paddle retries once the price is fixed.
func (h *Handler) saveSubscription(ctx context.Context, sub subscription) error {
	if sub.ID == "" || sub.CustomerID == "" || sub.UpdatedAt.IsZero() || len(sub.Items) == 0 {
		return fmt.Errorf("subscription %q: no id, customer_id, updated_at or items", sub.ID)
	}
	pr := sub.Items[0].Price
	if _, ok := paidPlan(pr); !ok && grantsAccess(sub.Status) {
		return fmt.Errorf("subscription %s: price %q has no known custom_data.taskwell_plan (%q)", sub.ID, pr.ID, pr.CustomData.Plan)
	}
	row := subscriptionRow{ID: sub.ID, CustomerID: sub.CustomerID, Status: sub.Status, PriceID: pr.ID, ProductID: pr.ProductID,
		Plan: pr.CustomData.Plan, NextBilledAt: sub.NextBilledAt, UpdatedAt: sub.UpdatedAt}
	if i := pr.BillingCycle.Interval; i != "" {
		row.Interval = &i
	}
	if c := strings.ToLower(sub.CurrencyCode); c != "" {
		row.Currency = &c
	}
	if a := pr.UnitPrice.Amount; a != "" {
		n, err := amount(a)
		if err != nil {
			return fmt.Errorf("subscription %s: price %s unit_price: %w", sub.ID, pr.ID, err)
		}
		row.UnitAmount = &n
	}
	if sc := sub.ScheduledChange; sc != nil && sc.Action != "" {
		row.ScheduledAction, row.ScheduledAt = &sc.Action, sc.EffectiveAt
	}
	link := h.link(sub.CustomData)
	applied, user, err := h.model.saveSubscription(ctx, row, link)
	if err != nil || !applied {
		return err
	}
	if user == nil {
		slog.WarnContext(ctx, "paddle subscription for no account", "subscription", sub.ID, "user", sub.CustomData.UserID,
			"signed", link != "")
		return nil
	}
	if sub.Status != "trialing" {
		return nil
	}
	return h.endRepeatTrial(ctx, sub.ID, *user)
}

// endRepeatTrial ends a trial on an account that had a subscription before (one trial per account): Paddle bills it
// now and it's active. The pricing page offers such an account the no-trial prices, so this only catches a checkout
// opened on a trial price some other way. An error makes Paddle retry the event.
func (h *Handler) endRepeatTrial(ctx context.Context, subID, userID string) error {
	had, err := h.model.hadEarlier(ctx, userID, subID)
	if err != nil || !had {
		return err
	}
	s, err := h.settings(ctx)
	if err != nil {
		return fmt.Errorf("end repeat trial %s: %w", subID, err)
	}
	var active subscription
	if err := h.paddle.call(ctx, s.key, s.env, http.MethodPost, "/subscriptions/"+url.PathEscape(subID)+"/activate", nil, &active); err != nil {
		return fmt.Errorf("end repeat trial %s: %w", subID, err)
	}
	slog.InfoContext(ctx, "paddle repeat trial ended", "subscription", subID, "user", userID)
	return h.saveSubscription(ctx, active)
}
