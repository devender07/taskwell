package workspace

import (
	"errors"
	"net/http"
	"strings"

	"github.com/devender07/taskwell/backend/user/internal/web"
)

const (
	errRoleNotFound  = "Role not found."
	errRoleNameTaken = "A role with that name already exists."
	errRoleAbove     = "You can't change a role that has permissions you don't have."
)

func (h *Handler) roles(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, "")
	if !ok || err != nil {
		return err
	}
	rs, err := h.model.roles(r.Context(), ws.ID)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, rs)
	return nil
}

func (h *Handler) createRole(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, permRolesManage)
	if !ok || err != nil {
		return err
	}
	name, perms, ok := readRoleForm(w, r, ws, true)
	if !ok {
		return nil
	}
	role, err := h.model.createRole(r.Context(), ws, userID, name, perms)
	if errors.Is(err, errNameTaken) {
		web.Fields(w, http.StatusConflict, map[string]string{"name": errRoleNameTaken})
		return nil
	}
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusCreated, role)
	return nil
}

// updateRole saves a custom role, or only the permissions of the Member role. Admin can't be changed.
func (h *Handler) updateRole(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, permRolesManage)
	if !ok || err != nil {
		return err
	}
	ctx := r.Context()
	role, ok, err := h.existingRole(w, r, ws, "The Admin role can't be changed.")
	if !ok || err != nil {
		return err
	}
	builtin := role.Builtin != nil
	name, perms, ok := readRoleForm(w, r, ws, !builtin)
	if !ok {
		return nil
	}
	if builtin {
		name = role.Name
	}
	updated, err := h.model.updateRole(ctx, ws, userID, role.ID, name, perms)
	if errors.Is(err, errNameTaken) {
		web.Fields(w, http.StatusConflict, map[string]string{"name": errRoleNameTaken})
		return nil
	}
	if err != nil {
		return err
	}
	if updated == nil {
		web.Err(w, http.StatusNotFound, errRoleNotFound)
		return nil
	}
	web.JSON(w, http.StatusOK, updated)
	return nil
}

func (h *Handler) deleteRole(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, permRolesManage)
	if !ok || err != nil {
		return err
	}
	role, ok, err := h.existingRole(w, r, ws, "Built-in roles can't be deleted.")
	if !ok || err != nil {
		return err
	}
	if role.Builtin != nil {
		web.Err(w, http.StatusForbidden, "Built-in roles can't be deleted.")
		return nil
	}
	deleted, err := h.model.deleteRole(r.Context(), ws, userID, role.ID)
	if err != nil {
		return err
	}
	if !deleted {
		web.Err(w, http.StatusNotFound, errRoleNotFound)
		return nil
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// existingRole returns the path's role. It writes a 404 when the workspace has no such role,
// a 403 with adminMsg for the Admin role, or a 403 when the role holds a permission the caller lacks
// (no reaching up), and returns ok=false.
func (h *Handler) existingRole(w http.ResponseWriter, r *http.Request, ws *Workspace, adminMsg string) (*Role, bool, error) {
	id := r.PathValue("id")
	if id == "" {
		web.Err(w, http.StatusNotFound, errRoleNotFound)
		return nil, false, nil
	}
	role, err := h.model.role(r.Context(), ws.ID, id)
	switch {
	case err != nil:
		return nil, false, err
	case role == nil:
		web.Err(w, http.StatusNotFound, errRoleNotFound)
		return nil, false, nil
	case role.Builtin != nil && *role.Builtin == roleAdmin:
		web.Err(w, http.StatusForbidden, adminMsg)
		return nil, false, nil
	case !subset(role.Permissions, ws.Permissions):
		web.Err(w, http.StatusForbidden, errRoleAbove)
		return nil, false, nil
	}
	return role, true, nil
}

// readRoleForm returns the trimmed name (checked when checkNameField) and the permissions in catalog order.
// It writes a 400 with field errors, or a 403 when the caller lacks a permission they're granting, and returns ok=false.
func readRoleForm(w http.ResponseWriter, r *http.Request, ws *Workspace, checkNameField bool) (string, []string, bool) {
	name := strings.TrimSpace(r.FormValue("name")) // FormValue parses the form, so r.Form below is filled
	errs := map[string]string{}
	if msg := checkRoleName(name); checkNameField && msg != "" {
		errs["name"] = msg
	}
	perms, ok := normPerms(r.Form["permissions"])
	if !ok {
		errs["permissions"] = "Unknown permission."
	}
	if len(errs) > 0 {
		web.Fields(w, http.StatusBadRequest, errs)
		return "", nil, false
	}
	if !subset(perms, ws.Permissions) {
		web.Err(w, http.StatusForbidden, errEscalation)
		return "", nil, false
	}
	return name, perms, true
}
