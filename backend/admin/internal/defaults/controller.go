package defaults

import (
	"cmp"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/devender07/taskwell/backend/admin/internal/web"
	"github.com/devender07/taskwell/backend/shared/doc"
	"github.com/devender07/taskwell/backend/shared/perms"
)

type Handler struct {
	model *model
	auth  web.Authenticator
}

func New(db conn, auth web.Authenticator) *Handler {
	return &Handler{model: &model{db: db}, auth: auth}
}

// check writes a 400 with the non-empty messages of errs and returns false, or returns true when there are none.
func check(w http.ResponseWriter, errs map[string]string) bool {
	out := map[string]string{}
	for k, v := range errs {
		if v != "" {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return true
	}
	web.WriteFields(w, http.StatusBadRequest, out)
	return false
}

// respond writes v (201 when the path has no id, else 200, or 204 for a nil v), or the answer to a model error:
// 404 notFound, 400 taken on name, 409 for the delete refusals, 400 on the field a missing reference was sent in.
func respond(w http.ResponseWriter, r *http.Request, v any, err error, notFound, taken string) error {
	switch {
	case errors.Is(err, errNotFound):
		web.WriteErr(w, http.StatusNotFound, notFound)
	case errors.Is(err, errNameTaken):
		web.WriteFields(w, http.StatusBadRequest, map[string]string{"name": taken})
	case errors.Is(err, errBuiltin):
		web.WriteErr(w, http.StatusConflict, "Built-in roles can't be deleted.")
	case errors.Is(err, errLastStatus):
		web.WriteErr(w, http.StatusConflict, "Keep at least one status.")
	case errors.Is(err, errBadOrder):
		web.WriteFields(w, http.StatusBadRequest, map[string]string{"id": errBadOrderMsg})
	case errors.Is(err, errNoType):
		web.WriteFields(w, http.StatusBadRequest, map[string]string{"type_id": errChooseType})
	case errors.Is(err, errNoStatus):
		web.WriteFields(w, http.StatusBadRequest, map[string]string{"status_id": errChooseStat})
	case errors.Is(err, errNoLabel):
		web.WriteFields(w, http.StatusBadRequest, map[string]string{"label_id": errChooseLabel})
	case err != nil:
		return err
	case v == nil:
		w.WriteHeader(http.StatusNoContent)
	case r.PathValue("id") == "":
		web.WriteJSON(w, http.StatusCreated, v)
	default:
		web.WriteJSON(w, http.StatusOK, v)
	}
	return nil
}

// optional is the trimmed form value, or nil when it's empty.
func optional(r *http.Request, key string) *string {
	if s := strings.TrimSpace(r.FormValue(key)); s != "" {
		return &s
	}
	return nil
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request, _ string) error {
	d, err := h.model.all(r.Context())
	if err != nil {
		return err
	}
	web.WriteJSON(w, http.StatusOK, d)
	return nil
}

// --- roles ---

// saveRole creates a custom role, or updates one (a built-in one's permissions only).
func (h *Handler) saveRole(w http.ResponseWriter, r *http.Request, adminID string) error {
	const notFound = "Role not found."
	id := r.PathValue("id")
	var builtin string
	if id != "" {
		role, err := h.model.role(r.Context(), id)
		if err != nil {
			return err
		}
		if role == nil {
			web.WriteErr(w, http.StatusNotFound, notFound)
			return nil
		}
		if role.Builtin != nil {
			builtin = *role.Builtin
		}
	}
	name := strings.TrimSpace(r.FormValue("name"))
	errs := map[string]string{}
	if builtin == "" {
		errs["name"] = checkRoleName(name)
		if strings.EqualFold(name, "admin") {
			errs["name"] = errRoleTaken // every workspace has an Admin role
		}
	}
	keys, ok := perms.Normalize(r.Form["permission"])
	switch {
	case !ok:
		errs["permission"] = errPermission
	case builtin == "guest" && slices.ContainsFunc(keys, perms.GuestDenied):
		errs["permission"] = errGuestPerm
	}
	if !check(w, errs) {
		return nil
	}
	role, err := h.model.saveRole(r.Context(), adminID, web.ClientIP(r), id, name, keys)
	return respond(w, r, role, err, notFound, errRoleTaken)
}

func (h *Handler) deleteRole(w http.ResponseWriter, r *http.Request, adminID string) error {
	err := h.model.deleteRole(r.Context(), adminID, web.ClientIP(r), r.PathValue("id"))
	return respond(w, r, nil, err, "Role not found.", "")
}

// --- status types ---

func (h *Handler) saveType(w http.ResponseWriter, r *http.Request, adminID string) error {
	name, category, icon := strings.TrimSpace(r.FormValue("name")), r.FormValue("category"), r.FormValue("icon")
	errs := map[string]string{"name": checkTypeName(name), "icon": checkTypeIcon(icon)}
	if checkCategory(category) != "" {
		errs["category"] = "Choose how it counts."
	}
	var color *string
	if c := normColor(r.FormValue("color")); c != "" {
		color = &c
		errs["color"] = checkColor(c)
	}
	if !check(w, errs) {
		return nil
	}
	t, err := h.model.saveType(r.Context(), adminID, web.ClientIP(r), r.PathValue("id"), name, category, icon, color)
	return respond(w, r, t, err, "Type not found.", errTypeTaken)
}

func (h *Handler) deleteType(w http.ResponseWriter, r *http.Request, adminID string) error {
	err := h.model.deleteType(r.Context(), adminID, web.ClientIP(r), r.PathValue("id"))
	return respond(w, r, nil, err, "Type not found.", "")
}

// --- statuses ---

func (h *Handler) saveStatus(w http.ResponseWriter, r *http.Request, adminID string) error {
	name, category := strings.TrimSpace(r.FormValue("name")), r.FormValue("category")
	typeID := optional(r, "type_id")
	errs := map[string]string{"name": checkStatusName(name)}
	if typeID == nil {
		errs["category"] = checkCategory(category)
	}
	if !check(w, errs) {
		return nil
	}
	s, err := h.model.saveStatus(r.Context(), adminID, web.ClientIP(r), r.PathValue("id"), name, category, typeID)
	return respond(w, r, s, err, "Status not found.", errStatusTaken)
}

func (h *Handler) deleteStatus(w http.ResponseWriter, r *http.Request, adminID string) error {
	err := h.model.deleteStatus(r.Context(), adminID, web.ClientIP(r), r.PathValue("id"))
	return respond(w, r, nil, err, "Status not found.", "")
}

func (h *Handler) reorderStatuses(w http.ResponseWriter, r *http.Request, adminID string) error {
	_ = r.FormValue("id") // parses the form
	out, err := h.model.reorderStatuses(r.Context(), adminID, web.ClientIP(r), r.Form["id"])
	if err == nil {
		web.WriteJSON(w, http.StatusOK, out)
		return nil
	}
	return respond(w, r, nil, err, "", "")
}

// sameSet reports whether a and b hold the same ids, each once.
func sameSet(a, b []string) bool {
	a, b = slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b))
	return slices.Equal(a, b) && len(slices.Compact(b)) == len(a)
}

// --- labels ---

func (h *Handler) saveLabel(w http.ResponseWriter, r *http.Request, adminID string) error {
	name, color := strings.TrimSpace(r.FormValue("name")), normColor(r.FormValue("color"))
	if !check(w, map[string]string{"name": checkLabelName(name), "color": checkColor(color)}) {
		return nil
	}
	l, err := h.model.saveLabel(r.Context(), adminID, web.ClientIP(r), r.PathValue("id"), name, color)
	return respond(w, r, l, err, "Label not found.", errLabelTaken)
}

func (h *Handler) deleteLabel(w http.ResponseWriter, r *http.Request, adminID string) error {
	err := h.model.deleteLabel(r.Context(), adminID, web.ClientIP(r), r.PathValue("id"))
	return respond(w, r, nil, err, "Label not found.", "")
}

// --- templates ---

func (h *Handler) saveTemplate(w http.ResponseWriter, r *http.Request, adminID string) error {
	f := templateForm{name: strings.TrimSpace(r.FormValue("name")), title: strings.TrimSpace(r.FormValue("title")),
		statusID: optional(r, "status_id")}
	errs := map[string]string{"name": checkTemplateName(f.name)}
	if doc.Len(f.title) > 200 {
		errs["title"] = "Title must be 200 characters or fewer."
	}
	if s := strings.TrimSpace(r.FormValue("description")); s != "" {
		f.description, errs["description"] = doc.Check([]byte(s), "Description", 50000)
	}
	n, err := strconv.Atoi(cmp.Or(strings.TrimSpace(r.FormValue("priority")), "0"))
	if f.priority = n; err != nil || n < 0 || n > 4 {
		errs["priority"] = "Choose a priority."
	}
	f.labelIDs = append([]string{}, slices.Compact(slices.Sorted(slices.Values(slices.DeleteFunc(slices.Clone(r.Form["label_id"]),
		func(s string) bool { return s == "" }))))...)
	if !check(w, errs) {
		return nil
	}
	t, err := h.model.saveTemplate(r.Context(), adminID, web.ClientIP(r), r.PathValue("id"), f)
	return respond(w, r, t, err, "Template not found.", "")
}

func (h *Handler) deleteTemplate(w http.ResponseWriter, r *http.Request, adminID string) error {
	err := h.model.deleteTemplate(r.Context(), adminID, web.ClientIP(r), r.PathValue("id"))
	return respond(w, r, nil, err, "Template not found.", "")
}
