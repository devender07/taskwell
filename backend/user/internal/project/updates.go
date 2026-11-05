package project

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/devender07/taskwell/backend/user/internal/doc"
	"github.com/devender07/taskwell/backend/user/internal/web"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

const errUpdateNotFound = "Update not found."

var healths = []string{"on_track", "at_risk", "off_track"}

// updates lists the project's updates a page at a time (?before= the oldest one's created_at).
func (h *Handler) updates(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, false)
	if !ok || err != nil {
		return err
	}
	var before *time.Time
	if v := r.URL.Query().Get("before"); v != "" {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			web.Err(w, http.StatusBadRequest, "before must be an RFC 3339 time.")
			return nil
		}
		before = &t
	}
	us, more, err := h.model.updates(r.Context(), ws.ID, normKey(r.PathValue("key")), userID, canManage(ws), before)
	if err != nil {
		return err
	}
	if us == nil {
		web.Err(w, http.StatusNotFound, errProjectNotFound)
		return nil
	}
	web.JSON(w, http.StatusOK, map[string]any{"updates": us, "has_more": more})
	return nil
}

func (h *Handler) createUpdate(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, false)
	if !ok || err != nil {
		return err
	}
	_ = r.ParseMultipartForm(maxBody)
	health, body := r.PostForm.Get("health"), r.PostForm.Get("body")
	d, bodyErr := checkUpdateBody(body)
	if !web.Check(w, map[string]string{"health": checkHealth(health), "body": bodyErr}) {
		return nil
	}
	u, err := h.model.createUpdate(r.Context(), ws.ID, normKey(r.PathValue("key")), userID, canManage(ws), health, d, event(ws, userID))
	switch {
	case errors.Is(err, errCantPost):
		web.Err(w, http.StatusForbidden, errForbidden)
	case errors.Is(err, errArchived):
		web.Err(w, http.StatusConflict, errArchivedProject)
	case err != nil:
		return err
	case u == nil:
		web.Err(w, http.StatusNotFound, errProjectNotFound)
	default:
		web.JSON(w, http.StatusCreated, u)
	}
	return nil
}

// editUpdate changes the fields sent: health, body or both.
func (h *Handler) editUpdate(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, id, ok, err := h.updateAccess(w, r, userID)
	if !ok || err != nil {
		return err
	}
	_ = r.ParseMultipartForm(maxBody)
	var health *string
	var body json.RawMessage
	errs := map[string]string{}
	if v, ok := r.PostForm["health"]; ok {
		health, errs["health"] = &v[0], checkHealth(v[0])
	}
	if v, ok := r.PostForm["body"]; ok {
		body, errs["body"] = checkUpdateBody(v[0])
	}
	if !web.Check(w, errs) {
		return nil
	}
	u, err := h.model.editUpdate(r.Context(), ws.ID, id, userID, canManage(ws), health, body, event(ws, userID))
	switch {
	case errors.Is(err, errCantEdit):
		web.Err(w, http.StatusForbidden, errForbidden)
	case err != nil:
		return err
	case u == nil:
		web.Err(w, http.StatusNotFound, errUpdateNotFound)
	default:
		web.JSON(w, http.StatusOK, u)
	}
	return nil
}

func (h *Handler) deleteUpdate(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, id, ok, err := h.updateAccess(w, r, userID)
	if !ok || err != nil {
		return err
	}
	found, err := h.model.deleteUpdate(r.Context(), ws.ID, id, userID, canManage(ws), event(ws, userID))
	switch {
	case errors.Is(err, errCantEdit):
		web.Err(w, http.StatusForbidden, errForbidden)
	case err != nil:
		return err
	case !found:
		web.Err(w, http.StatusNotFound, errUpdateNotFound)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
	return nil
}

// updateAccess is access for a write to the path's update; it writes a 404 when its id isn't a UUID.
func (h *Handler) updateAccess(w http.ResponseWriter, r *http.Request, userID string) (*workspace.Workspace, string, bool, error) {
	ws, ok, err := h.access(w, r, userID, false)
	if !ok || err != nil {
		return nil, "", false, err
	}
	id := r.PathValue("id")
	if !web.IsUUID(id) {
		web.Err(w, http.StatusNotFound, errUpdateNotFound)
		return nil, "", false, nil
	}
	return ws, id, true, nil
}

func canManage(ws *workspace.Workspace) bool {
	return workspace.Has(ws.Permissions, workspace.PermProjectsManage)
}

func checkHealth(s string) string {
	if !slices.Contains(healths, s) {
		return "Choose a health."
	}
	return ""
}

// checkUpdateBody is doc.Check for an update's body, which can't be empty.
func checkUpdateBody(raw string) (json.RawMessage, string) {
	if raw == "" {
		return nil, "Write an update."
	}
	d, msg := doc.Check([]byte(raw), "Update", 10000)
	if d == nil && msg == "" {
		msg = "Write an update."
	}
	return d, msg
}
