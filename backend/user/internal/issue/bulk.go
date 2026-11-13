package issue

import (
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/devender07/taskwell/backend/user/internal/web"
)

// maxBulk caps a bulk request's issues.
const maxBulk = 250

// bulkFields are the changes a bulk request can make; delete is the other kind.
var bulkFields = []string{"status_id", "assignee_id", "priority", "cycle", "add_label_ids", "remove_label_ids"}

// bulk changes or deletes several issues at once, all or none: each must pass the single endpoint's checks.
func (h *Handler) bulk(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, "")
	if !ok || err != nil {
		return err
	}
	_ = r.ParseMultipartForm(maxBody)
	form := r.PostForm
	var identifiers []string
	for _, s := range form["ids"] {
		if s = strings.ToUpper(strings.TrimSpace(s)); s != "" && !slices.Contains(identifiers, s) {
			identifiers = append(identifiers, s)
		}
	}
	if len(identifiers) == 0 || len(identifiers) > maxBulk {
		web.Err(w, http.StatusBadRequest, "Pick some issues.")
		return nil
	}
	del := form.Get("delete") == "1"
	changes := slices.ContainsFunc(bulkFields, func(k string) bool { return form.Has(k) })
	switch {
	case del && changes:
		web.Err(w, http.StatusBadRequest, "Delete can't be combined with other changes.")
		return nil
	case !del && !changes:
		web.Err(w, http.StatusBadRequest, "Choose something to change.")
		return nil
	}
	is, err := h.model.findMany(r.Context(), ws.ID, identifiers, userID)
	if err != nil {
		return err
	}
	if is == nil {
		web.Err(w, http.StatusBadRequest, "Some issues weren't found.")
		return nil
	}
	var ids, keys []string
	for i := range is {
		allow(&is[i], userID, ws.Permissions)
		if del && !is[i].CanDelete || !del && !is[i].CanEdit {
			verb := map[bool]string{true: "delete", false: "edit"}[del]
			if is[i].Project.Archived {
				web.Err(w, http.StatusForbidden, is[i].Identifier+" is in an archived project.")
				return nil
			}
			web.Err(w, http.StatusForbidden, "You can't "+verb+" "+is[i].Identifier+".")
			return nil
		}
		if !slices.Contains(ids, is[i].ID) {
			ids = append(ids, is[i].ID)
		}
		if !slices.Contains(keys, is[i].Project.Key) {
			keys = append(keys, is[i].Project.Key)
		}
	}
	if del {
		if err := h.model.bulkDelete(r.Context(), ids, userID); err != nil {
			return err
		}
		web.JSON(w, http.StatusOK, map[string]int{"deleted": len(ids)})
		return nil
	}

	// The status, assignee, priority and cycle are checked like a single update's, against each project.
	var f fields
	for _, key := range keys {
		if f, ok, err = h.readFields(w, r, ws.ID, key, "", false); !ok || err != nil {
			return err
		}
	}
	f = fields{statusID: f.statusID, assigneeID: f.assigneeID, priority: f.priority, cycleID: f.cycleID}
	add, remove := labelIDs(form["add_label_ids"]), labelIDs(form["remove_label_ids"])
	errs := map[string]string{}
	for k, ls := range map[string][]string{"add_label_ids": add, "remove_label_ids": remove} {
		if slices.ContainsFunc(ls, func(s string) bool { return !web.IsUUID(s) }) {
			errs[k] = "Choose labels from this workspace."
			continue
		}
		refErrs, err := h.model.checkRefs(r.Context(), ws.ID, "", "", fields{labelIDs: ls})
		if err != nil {
			return err
		}
		errs[k] = refErrs["label_ids"]
	}
	if !web.Check(w, errs) {
		return nil
	}
	err = h.model.bulkUpdate(r.Context(), ws.ID, ids, userID, f, add, remove)
	if errors.Is(err, errNoCycle) {
		web.Fields(w, http.StatusBadRequest, map[string]string{"cycle": errCycleNotFound})
		return nil
	}
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]int{"updated": len(ids)})
	return nil
}

// labelIDs are the form's label ids without blanks and duplicates, sorted.
func labelIDs(ids []string) []string {
	return slices.Compact(slices.Sorted(slices.Values(slices.DeleteFunc(slices.Clone(ids), func(s string) bool { return s == "" }))))
}

// labelSet is the labels have plus add, less remove, sorted.
func labelSet(have, add, remove []string) []string {
	return slices.DeleteFunc(labelIDs(slices.Concat(have, add)), func(s string) bool { return slices.Contains(remove, s) })
}
