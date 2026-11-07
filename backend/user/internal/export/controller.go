package export

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/secret"
	"github.com/devender07/taskwell/backend/user/internal/storage"
	"github.com/devender07/taskwell/backend/user/internal/web"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

const (
	errNoWorkspace = "Workspace not found."
	errForbidden   = "You don't have permission to export this workspace."
	errTooMany     = "Too many exports. Try again in a few minutes."
	errExpired     = "This export link has expired. Start a new export."
)

type Handler struct {
	model        *model
	files        *storage.Store
	key          []byte
	lim          web.Limiter
	now          func() time.Time
	authenticate func(*http.Request) (string, error)
	member       func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)
}

// New takes the JWT secret (download tokens are signed with a key derived from it), the auth handler's Authenticate
// and the workspace handler's Member.
func New(db *pgxpool.Pool, files *storage.Store, jwtSecret string, authenticate func(*http.Request) (string, error),
	member func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)) *Handler {
	return &Handler{model: &model{db: db}, files: files, key: secret.Key("taskwell exports", "jwt_secret:"+jwtSecret), now: time.Now,
		lim:          web.Limiter{DB: db, Name: "export", Burst: 3, Per: 20 * time.Minute},
		authenticate: authenticate, member: member}
}

// start answers a download link for the workspace's export, good for tokenTTL. 3 per workspace per hour.
func (h *Handler) start(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, err := h.member(r.Context(), userID, r.PathValue("slug"))
	switch {
	case err != nil:
		return err
	case ws == nil:
		web.Err(w, http.StatusNotFound, errNoWorkspace)
		return nil
	case !workspace.Has(ws.Permissions, workspace.PermWorkspaceExport):
		web.Err(w, http.StatusForbidden, errForbidden)
		return nil
	}
	wait, err := h.lim.Take(r.Context(), ws.ID)
	if err != nil {
		return err
	}
	if wait > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		web.Err(w, http.StatusTooManyRequests, errTooMany)
		return nil
	}
	web.JSON(w, http.StatusOK, map[string]string{"url": "/api/exports/" + sign(h.key, ws.ID, userID, h.now().Add(tokenTTL))})
	return nil
}

// download streams the zip for a valid token whose user is still a member with workspace.export.
func (h *Handler) download(w http.ResponseWriter, r *http.Request) error {
	wsID, userID, ok := verify(h.key, r.PathValue("token"), h.now())
	if !ok {
		web.Err(w, http.StatusNotFound, errExpired)
		return nil
	}
	slug, err := h.model.slug(r.Context(), wsID)
	if err != nil {
		return err
	}
	var ws *workspace.Workspace
	if slug != "" {
		if ws, err = h.member(r.Context(), userID, slug); err != nil {
			return err
		}
	}
	if ws == nil || ws.ID != wsID || !workspace.Has(ws.Permissions, workspace.PermWorkspaceExport) {
		web.Err(w, http.StatusNotFound, errExpired)
		return nil
	}
	e, err := h.model.load(r.Context(), wsID, userID)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+ws.Slug+"-export-"+e.ExportedAt.Format(time.DateOnly)+`.zip"`)
	w.Header().Set("Cache-Control", "no-store")
	if err := h.writeZip(r.Context(), w, e); err != nil {
		// The 200 is out; abort the connection so the browser shows a failed download, not a truncated zip.
		slog.ErrorContext(r.Context(), "export failed", "workspace", wsID, "err", err)
		panic(http.ErrAbortHandler)
	}
	return nil
}
