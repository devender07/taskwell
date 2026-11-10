package feature

import (
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/web"
)

// MFARequired is the error a member without two-factor gets from a workspace that requires it.
const MFARequired = "This workspace requires two-factor sign-in."

// RequireMFA answers 403 {"error": MFARequired, "mfa_required": true} to a /api/workspaces/{slug}/… request (cookie or
// API token) from a member without two-factor sign-in when the workspace requires it. They can still open the
// workspace (GET /api/workspaces/{slug}), read its features and billing, and leave it.
// ponytail: signs the request in a second time (the handler does too); pass the user on in the context if the extra
// token lookup shows up in DB load.
func (f *Flags) RequireMFA(authenticate func(*http.Request) (string, error), next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seg := strings.Split(r.URL.Path, "/") // "", "api", "workspaces", slug, ...
		if len(seg) < 4 || seg[1] != "api" || seg[2] != "workspaces" || seg[3] == "" || mfaExempt(r.Method, seg, "") {
			next.ServeHTTP(w, r)
			return
		}
		userID, err := authenticate(r)
		if err != nil {
			web.Fail(w, r, err)
			return
		}
		if userID == "" || mfaExempt(r.Method, seg, userID) {
			next.ServeHTTP(w, r)
			return
		}
		var blocked bool
		err = f.db.QueryRow(r.Context(), `select true from workspaces w
			join workspace_members m on m.workspace_id = w.id and m.user_id = $2
			where w.slug = lower($1) and w.require_mfa
			  and not exists (select 1 from user_mfa where user_id = $2 and enabled_at is not null)`, seg[3], userID).Scan(&blocked)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			web.Fail(w, r, err)
			return
		}
		if blocked {
			web.JSON(w, http.StatusForbidden, map[string]any{"error": MFARequired, "mfa_required": true})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// mfaExempt reports whether a blocked member may still make the request: open the workspace, read its features or
// billing, or leave it (DELETE …/members/{their id}; userID "" before they're known).
func mfaExempt(method string, seg []string, userID string) bool {
	rest := seg[4:]
	switch {
	case method == http.MethodGet && len(rest) == 0:
		return true
	case method == http.MethodGet && len(rest) == 1 && (rest[0] == "features" || rest[0] == "billing"):
		return true
	case method == http.MethodDelete && len(rest) == 2 && rest[0] == "members" && userID != "" && rest[1] == userID:
		return true
	}
	return false
}
