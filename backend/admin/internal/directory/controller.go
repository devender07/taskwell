package directory

import (
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/admin/internal/web"
)

const (
	pageSize = 50
	maxQuery = 200 // search text is cut to this many characters
)

type Handler struct {
	model  *model
	auth   web.Authenticator
	stripe string // Stripe's API base; tests point it at httptest
}

func New(db *pgxpool.Pool, auth web.Authenticator) *Handler {
	return &Handler{model: &model{db: db}, auth: auth, stripe: stripeBase}
}

// search reads ?q= (cut to maxQuery characters) and the 1-based ?page= (anything invalid is page 1) as a query and an offset.
func search(r *http.Request) (string, int) {
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 1 {
		page = 1
	}
	q := []rune(strings.TrimSpace(r.URL.Query().Get("q")))
	return string(q[:min(len(q), maxQuery)]), (page - 1) * pageSize
}

func (h *Handler) workspaces(w http.ResponseWriter, r *http.Request, _ string) error {
	q, offset := search(r)
	ws, total, err := h.model.workspaces(r.Context(), q, offset)
	if err != nil {
		return err
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"workspaces": ws, "total": total})
	return nil
}

func (h *Handler) users(w http.ResponseWriter, r *http.Request, _ string) error {
	q, offset := search(r)
	us, total, err := h.model.users(r.Context(), q, offset)
	if err != nil {
		return err
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"users": us, "total": total})
	return nil
}

func (h *Handler) suspend(w http.ResponseWriter, r *http.Request, adminID string) error {
	if r.PathValue("id") == adminID {
		web.WriteErr(w, http.StatusBadRequest, "You can't suspend yourself.")
		return nil
	}
	return h.setSuspended(w, r, adminID, true)
}

func (h *Handler) unsuspend(w http.ResponseWriter, r *http.Request, adminID string) error {
	return h.setSuspended(w, r, adminID, false)
}

func (h *Handler) setSuspended(w http.ResponseWriter, r *http.Request, adminID string, suspended bool) error {
	var id pgtype.UUID
	var u *User
	if id.Scan(r.PathValue("id")) == nil {
		var err error
		if u, err = h.model.setSuspended(r.Context(), adminID, web.ClientIP(r), r.PathValue("id"), suspended); err != nil {
			return err
		}
	}
	if u == nil {
		web.WriteErr(w, http.StatusNotFound, "User not found.")
		return nil
	}
	web.WriteJSON(w, http.StatusOK, u)
	return nil
}

// setPlan comps the workspace with the form's plan (comped=true), or clears its comp (comped=false): back to its
// Stripe subscription's plan while one is active, else Starter.
func (h *Handler) setPlan(w http.ResponseWriter, r *http.Request, adminID string) error {
	ctx := r.Context()
	var id pgtype.UUID
	if id.Scan(r.PathValue("id")) != nil {
		web.WriteErr(w, http.StatusNotFound, "Workspace not found.")
		return nil
	}
	plan, comped := r.FormValue("plan"), r.FormValue("comped")
	switch {
	case comped != "true" && comped != "false":
		web.WriteErr(w, http.StatusBadRequest, "Choose whether the plan is comped.")
		return nil
	case comped == "true" && !slices.Contains([]string{"starter", "pro", "ultimate"}, plan):
		web.WriteErr(w, http.StatusBadRequest, "Choose a plan.")
		return nil
	}
	if comped == "false" {
		subID, status, found, err := h.model.subscription(ctx, r.PathValue("id"))
		if err != nil {
			return err
		}
		if !found {
			web.WriteErr(w, http.StatusNotFound, "Workspace not found.")
			return nil
		}
		plan = "starter"
		if subID != nil && status != nil && slices.Contains([]string{"active", "trialing", "past_due"}, *status) {
			key, err := h.model.stripeKey(ctx)
			if err != nil {
				return err
			}
			if plan, err = subscriptionPlan(ctx, h.stripe, key, *subID); err != nil {
				slog.WarnContext(ctx, "read stripe subscription", "subscription", *subID, "err", err)
				web.WriteErr(w, http.StatusBadGateway, "Couldn't read the workspace's subscription from Stripe. Try again.")
				return nil
			}
		}
	}
	ws, err := h.model.setPlan(ctx, adminID, web.ClientIP(r), r.PathValue("id"), plan, comped == "true")
	if err != nil {
		return err
	}
	if ws == nil {
		web.WriteErr(w, http.StatusNotFound, "Workspace not found.")
		return nil
	}
	web.WriteJSON(w, http.StatusOK, ws)
	return nil
}
