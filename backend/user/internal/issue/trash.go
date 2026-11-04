package issue

import (
	"net/http"
	"time"

	"github.com/devender07/taskwell/backend/user/internal/web"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

// trash lists a page of the workspace's trashed issues the caller can see (?before= the last one's deleted_at).
func (h *Handler) trash(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, "")
	if !ok || err != nil {
		return err
	}
	var before time.Time
	if b := r.URL.Query().Get("before"); b != "" {
		if before, err = time.Parse(time.RFC3339Nano, b); err != nil {
			web.Err(w, http.StatusBadRequest, "Invalid before.")
			return nil
		}
	}
	ts, more, err := h.model.trash(r.Context(), ws.ID, userID, before)
	if err != nil {
		return err
	}
	editAny := workspace.Has(ws.Permissions, workspace.PermIssuesEditAny)
	for i := range ts {
		ts[i].CanRestore = editAny || ts[i].createdBy == userID
	}
	web.JSON(w, http.StatusOK, map[string]any{"issues": ts, "has_more": more})
	return nil
}

// trashed returns the path's workspace and trashed issue, or writes a 404 (or a 403 without perm) and returns
// ok=false.
func (h *Handler) trashed(w http.ResponseWriter, r *http.Request, userID, perm string) (*workspace.Workspace, *Trashed, bool, error) {
	ws, ok, err := h.access(w, r, userID, perm)
	if !ok || err != nil {
		return nil, nil, false, err
	}
	var t *Trashed
	if key, number, ok := ParseIdentifier(r.PathValue("identifier")); ok {
		if t, err = h.model.trashed(r.Context(), ws.ID, key, number, userID); err != nil {
			return nil, nil, false, err
		}
	}
	if t == nil {
		web.Err(w, http.StatusNotFound, errIssueNotFound)
		return nil, nil, false, nil
	}
	return ws, t, true, nil
}

// restore takes an issue out of the trash; who may delete it may restore it.
func (h *Handler) restore(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, t, ok, err := h.trashed(w, r, userID, "")
	if !ok || err != nil {
		return err
	}
	if !workspace.Has(ws.Permissions, workspace.PermIssuesEditAny) && t.createdBy != userID {
		web.Err(w, http.StatusForbidden, errForbidden)
		return nil
	}
	if t.archived {
		web.Err(w, http.StatusConflict, "The project is archived.")
		return nil
	}
	if ok, err := h.model.restore(r.Context(), t.ID, userID); err != nil || !ok {
		if err == nil {
			web.Err(w, http.StatusNotFound, errIssueNotFound)
		}
		return err
	}
	return h.respondIssue(w, r, http.StatusOK, t.ID, userID, ws)
}

// purge deletes one trashed issue for good.
func (h *Handler) purge(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, t, ok, err := h.trashed(w, r, userID, workspace.PermIssuesEditAny)
	if !ok || err != nil {
		return err
	}
	if err := h.model.purge(r.Context(), ws.ID, ws.Slug, userID, t.ID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// emptyTrash deletes for good every trashed issue the caller can see.
func (h *Handler) emptyTrash(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, workspace.PermIssuesEditAny)
	if !ok || err != nil {
		return err
	}
	if err := h.model.purge(r.Context(), ws.ID, ws.Slug, userID, ""); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
