package directory

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/admin/internal/audit"
	"github.com/devender07/taskwell/backend/admin/internal/web"
)

const (
	pageSize = 50
	maxQuery = 200 // search text is cut to this many characters
)

type Handler struct {
	model *model
	auth  web.Authenticator
}

func New(db *pgxpool.Pool, auth web.Authenticator) *Handler {
	return &Handler{model: &model{db: db}, auth: auth}
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

// paidPlans are the user API's paid plan ids (its package plan), free is the plan without a subscription.
var paidPlans = []string{"starter", "pro", "advanced"}

const freePlan = "free"

// setPlan comps the user's account with the form's plan (comped=true), for the form's days (none: no end), or clears
// its comp (comped=false): back to the plan of its mirrored Paddle subscription granting access, else Free.
func (h *Handler) setPlan(w http.ResponseWriter, r *http.Request, adminID string) error {
	ctx := r.Context()
	var id pgtype.UUID
	if id.Scan(r.PathValue("id")) != nil {
		web.WriteErr(w, http.StatusNotFound, "User not found.")
		return nil
	}
	plan, comped := r.FormValue("plan"), r.FormValue("comped")
	days := 0
	if d := r.FormValue("days"); comped == "true" && d != "" {
		var err error
		if days, err = strconv.Atoi(d); err != nil || days < 1 || days > 365 {
			web.WriteErr(w, http.StatusBadRequest, "Choose 1 to 365 days.")
			return nil
		}
	}
	switch {
	case comped != "true" && comped != "false":
		web.WriteErr(w, http.StatusBadRequest, "Choose whether the plan is comped.")
		return nil
	case comped == "true" && plan != freePlan && !slices.Contains(paidPlans, plan):
		web.WriteErr(w, http.StatusBadRequest, "Choose a plan.")
		return nil
	}
	u, err := h.model.setPlan(ctx, adminID, web.ClientIP(r), r.PathValue("id"), plan, comped == "true", days)
	if err != nil {
		return err
	}
	if u == nil {
		web.WriteErr(w, http.StatusNotFound, "User not found.")
		return nil
	}
	web.WriteJSON(w, http.StatusOK, u)
	return nil
}

// user answers the user's detail page.
func (h *Handler) user(w http.ResponseWriter, r *http.Request, _ string) error {
	var id pgtype.UUID
	var d *UserDetail
	if id.Scan(r.PathValue("id")) == nil {
		var err error
		if d, err = h.model.user(r.Context(), r.PathValue("id")); err != nil {
			return err
		}
	}
	if d == nil {
		web.WriteErr(w, http.StatusNotFound, "User not found.")
		return nil
	}
	web.WriteJSON(w, http.StatusOK, d)
	return nil
}

// usersCSV downloads every user matching ?q=, recorded in admin_audit.
func (h *Handler) usersCSV(w http.ResponseWriter, r *http.Request, adminID string) error {
	q, _ := search(r)
	us, err := h.model.allUsers(r.Context(), q)
	if err != nil {
		return err
	}
	if err := audit.Record(r.Context(), h.model.db, adminID, audit.UsersExported, q, web.ClientIP(r)); err != nil {
		return err
	}
	at := func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return t.UTC().Format(time.RFC3339)
	}
	rows := make([][]string, len(us))
	for i, u := range us {
		rows[i] = []string{u.ID, u.Name, u.Email, u.Plan, strconv.FormatBool(u.Comped), at(u.CompedUntil),
			strconv.FormatBool(u.Suspended), strconv.FormatBool(u.MFA), strconv.Itoa(u.Workspaces), at(&u.CreatedAt), at(u.LastActiveAt)}
	}
	web.WriteCSV(w, "users.csv", []string{"id", "name", "email", "plan", "comped", "comped_until", "suspended", "mfa",
		"workspaces", "created_at", "last_active_at"}, rows)
	return nil
}
