package issue

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/user/internal/mail"
	"github.com/devender07/tether/backend/user/internal/sysconfig"
	"github.com/devender07/tether/backend/user/internal/workspace"
)

const (
	errNotFound        = "Workspace not found."
	errProjectNotFound = "Project not found."
	errIssueNotFound   = "Issue not found."
	errStatusNotFound  = "Status not found."
	errLabelNotFound   = "Label not found."
	errForbidden       = "You don't have permission to do that."
	errCommentNotFound = "Comment not found."
	errArchivedProject = "This project is archived."
)

type Handler struct {
	model        *model
	authenticate func(*http.Request) (string, error)
	member       func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)
	mail         *mail.Mailer
	cfg          *sysconfig.Store
	hub          *hub
}

// New takes the auth handler's Authenticate and the workspace handler's Member.
func New(db *pgxpool.Pool, authenticate func(*http.Request) (string, error),
	member func(ctx context.Context, userID, slug string) (*workspace.Workspace, error),
	mailer *mail.Mailer, cfg *sysconfig.Store) *Handler {
	return &Handler{model: &model{db: db}, authenticate: authenticate, member: member, mail: mailer, cfg: cfg,
		hub: &hub{rooms: map[string]*room{}}}
}

// --- issues ---

func (h *Handler) list(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, "")
	if !ok || err != nil {
		return err
	}
	projectID, err := h.model.projectID(r.Context(), ws.ID, normKey(r.PathValue("key")))
	if err != nil {
		return err
	}
	if projectID == "" {
		writeErr(w, http.StatusNotFound, errProjectNotFound)
		return nil
	}
	is, err := h.model.list(r.Context(), projectID)
	if err != nil {
		return err
	}
	for i := range is {
		allow(&is[i], userID, ws.Permissions)
	}
	writeJSON(w, http.StatusOK, is)
	return nil
}

// myIssues lists the caller's assigned issues across the workspace's active projects.
func (h *Handler) myIssues(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, "")
	if !ok || err != nil {
		return err
	}
	is, err := h.model.assigned(r.Context(), ws.ID, userID)
	if err != nil {
		return err
	}
	for i := range is {
		allow(&is[i], userID, ws.Permissions)
	}
	writeJSON(w, http.StatusOK, is)
	return nil
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, workspace.PermIssuesCreate)
	if !ok || err != nil {
		return err
	}
	f, ok, err := h.readFields(w, r, ws.ID, true)
	if !ok || err != nil {
		return err
	}
	id, err := h.model.create(r.Context(), ws.ID, normKey(r.PathValue("key")), userID, f)
	if errors.Is(err, errArchived) {
		writeErr(w, http.StatusConflict, errArchivedProject)
		return nil
	}
	if !checkParent(w, err) {
		return nil
	}
	if err != nil {
		return err
	}
	if id == "" {
		writeErr(w, http.StatusNotFound, errProjectNotFound)
		return nil
	}
	h.notifyMentions(r.Context(), id, userID, nil, f.description, false)
	return h.respondIssue(w, r, http.StatusCreated, id, userID, ws)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, i, ok, err := h.issue(w, r, userID)
	if !ok || err != nil {
		return err
	}
	return h.respondIssue(w, r, http.StatusOK, i.ID, userID, ws)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, i, ok, err := h.editable(w, r, userID)
	if !ok || err != nil {
		return err
	}
	f, ok, err := h.readFields(w, r, ws.ID, false)
	if !ok || err != nil {
		return err
	}
	old, err := h.model.update(r.Context(), ws.ID, i.ID, userID, f)
	if !checkParent(w, err) {
		return nil
	}
	if err != nil {
		return err
	}
	if f.setDescription {
		h.notifyMentions(r.Context(), i.ID, userID, old, f.description, false)
	}
	return h.respondIssue(w, r, http.StatusOK, i.ID, userID, ws)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, i, ok, err := h.issue(w, r, userID)
	if !ok || err != nil {
		return err
	}
	if allow(i, userID, ws.Permissions); !i.CanDelete {
		writeErr(w, http.StatusForbidden, errForbidden)
		return nil
	}
	if err := h.model.delete(r.Context(), i.ID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) search(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, "")
	if !ok || err != nil {
		return err
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	key, number, _ := parseIdentifier(q)
	refs, err := h.model.search(r.Context(), ws.ID, normKey(r.URL.Query().Get("project")), q, key, number)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, refs)
	return nil
}

func (h *Handler) link(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, i, ok, err := h.editable(w, r, userID)
	if !ok || err != nil {
		return err
	}
	_ = r.ParseMultipartForm(maxBody)
	typ := r.PostForm.Get("type")
	other, err := h.find(r.Context(), ws.ID, r.PostForm.Get("issue"))
	if err != nil {
		return err
	}
	errs := map[string]string{}
	if typ != "blocks" && typ != "blocked_by" {
		errs["type"] = "Choose a link type."
	}
	switch {
	case other == nil:
		errs["issue"] = errIssueNotFound
	case other.ID == i.ID:
		errs["issue"] = "An issue can't block itself."
	}
	if !checkFields(w, errs) {
		return nil
	}
	blocker, blocked := i.ID, other.ID
	if typ == "blocked_by" {
		blocker, blocked = blocked, blocker
	}
	err = h.model.link(r.Context(), blocker, blocked, userID)
	if errors.Is(err, errLinked) {
		writeFields(w, http.StatusConflict, map[string]string{"issue": "These issues are already linked."})
		return nil
	}
	if err != nil {
		return err
	}
	return h.respondIssue(w, r, http.StatusCreated, i.ID, userID, ws)
}

func (h *Handler) unlink(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, i, ok, err := h.editable(w, r, userID)
	if !ok || err != nil {
		return err
	}
	other, err := h.find(r.Context(), ws.ID, r.PathValue("other"))
	if err != nil {
		return err
	}
	if other == nil {
		writeErr(w, http.StatusNotFound, errIssueNotFound)
		return nil
	}
	found, err := h.model.unlink(r.Context(), i.ID, other.ID, userID)
	if err != nil {
		return err
	}
	if !found {
		writeErr(w, http.StatusNotFound, "Link not found.")
		return nil
	}
	return h.respondIssue(w, r, http.StatusOK, i.ID, userID, ws)
}

// find returns the workspace's issue with the identifier; nil when there's none.
func (h *Handler) find(ctx context.Context, workspaceID, identifier string) (*Issue, error) {
	key, number, ok := parseIdentifier(identifier)
	if !ok {
		return nil, nil
	}
	return h.model.get(ctx, workspaceID, key, number)
}

// editable is issue, plus a 403 unless the caller may edit it.
func (h *Handler) editable(w http.ResponseWriter, r *http.Request, userID string) (*workspace.Workspace, *Issue, bool, error) {
	ws, i, ok, err := h.issue(w, r, userID)
	if !ok || err != nil {
		return nil, nil, false, err
	}
	if allow(i, userID, ws.Permissions); !i.CanEdit {
		writeErr(w, http.StatusForbidden, errForbidden)
		return nil, nil, false, nil
	}
	return ws, i, true, nil
}

// issue returns the path's workspace and issue, or writes a 404 and returns ok=false.
func (h *Handler) issue(w http.ResponseWriter, r *http.Request, userID string) (*workspace.Workspace, *Issue, bool, error) {
	ws, ok, err := h.access(w, r, userID, "")
	if !ok || err != nil {
		return nil, nil, false, err
	}
	i, err := h.find(r.Context(), ws.ID, r.PathValue("identifier"))
	if err != nil {
		return nil, nil, false, err
	}
	if i == nil {
		writeErr(w, http.StatusNotFound, errIssueNotFound)
		return nil, nil, false, nil
	}
	return ws, i, true, nil
}

// respondIssue writes the issue with the id and its relations as the caller sees it.
func (h *Handler) respondIssue(w http.ResponseWriter, r *http.Request, status int, id, userID string, ws *workspace.Workspace) error {
	i, err := h.model.byID(r.Context(), id)
	if err != nil {
		return err
	}
	if i == nil {
		writeErr(w, http.StatusNotFound, errIssueNotFound)
		return nil
	}
	allow(i, userID, ws.Permissions)
	d, err := h.model.relations(r.Context(), i)
	if err != nil {
		return err
	}
	writeJSON(w, status, d)
	return nil
}

// checkParent writes a 400 for a parentError and returns false; true for any other err, nil included.
func checkParent(w http.ResponseWriter, err error) bool {
	var pe parentError
	if errors.As(err, &pe) {
		writeFields(w, http.StatusBadRequest, map[string]string{"parent": string(pe)})
		return false
	}
	return true
}

// allow sets the caller's can_edit and can_delete: issues.edit_any, or they created it; editing also when it's
// assigned to them.
func allow(i *Issue, userID string, perms []string) {
	editAny := workspace.Has(perms, workspace.PermIssuesEditAny)
	creator := i.CreatedBy != nil && i.CreatedBy.ID == userID
	i.CanDelete = editAny || creator
	i.CanEdit = i.CanDelete || i.Assignee != nil && i.Assignee.ID == userID
}

// readFields returns the issue fields present in the form, or writes a 400 with field errors and returns ok=false.
// On create the title is always checked, and an empty status_id or priority means the default.
func (h *Handler) readFields(w http.ResponseWriter, r *http.Request, workspaceID string, create bool) (fields, bool, error) {
	_ = r.ParseMultipartForm(maxBody)
	form := r.PostForm
	value := func(k string) *string {
		v, ok := form[k]
		if !ok || create && v[0] == "" && (k == "status_id" || k == "priority" || k == "parent") {
			return nil
		}
		return &v[0]
	}
	var f fields
	errs := map[string]string{}
	if t := value("title"); t != nil || create {
		title := strings.TrimSpace(form.Get("title"))
		f.title, errs["title"] = &title, checkTitle(title)
	}
	if d := value("description"); d != nil {
		f.setDescription = true
		if *d != "" {
			f.description, errs["description"] = checkDescription([]byte(*d))
		}
	}
	if f.statusID = value("status_id"); f.statusID != nil && *f.statusID == "" {
		f.statusID, errs["status_id"] = nil, "Choose a status."
	}
	if p := value("priority"); p != nil {
		n, _ := parsePriority(*p)
		f.priority, errs["priority"] = &n, checkPriority(*p)
	}
	if f.dueDate = value("due_date"); f.dueDate != nil {
		errs["due_date"] = checkDueDate(*f.dueDate)
	}
	f.assigneeID = value("assignee_id")
	f.parent = value("parent")
	if ids, ok := form["label_ids"]; ok {
		f.setLabels = true
		f.labelIDs = slices.Compact(slices.Sorted(slices.Values(slices.DeleteFunc(slices.Clone(ids), func(s string) bool { return s == "" }))))
	}
	if !checkFields(w, errs) {
		return f, false, nil
	}
	refErrs, err := h.model.checkRefs(r.Context(), workspaceID, f)
	if err != nil || !checkFields(w, refErrs) {
		return f, false, err
	}
	return f, true, nil
}

// --- comments and activity ---

func (h *Handler) timeline(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, i, ok, err := h.issue(w, r, userID)
	if !ok || err != nil {
		return err
	}
	cs, es, err := h.model.timeline(r.Context(), i.ID)
	if err != nil {
		return err
	}
	for i := range cs {
		allowComment(&cs[i], userID, ws.Permissions)
	}
	writeJSON(w, http.StatusOK, map[string]any{"comments": cs, "events": es})
	return nil
}

func (h *Handler) createComment(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, i, ok, err := h.issue(w, r, userID)
	if !ok || err != nil {
		return err
	}
	if !workspace.Has(ws.Permissions, workspace.PermCommentsCreate) {
		writeErr(w, http.StatusForbidden, errForbidden)
		return nil
	}
	archived, err := h.model.archived(r.Context(), i.ID)
	if err != nil {
		return err
	}
	if archived {
		writeErr(w, http.StatusForbidden, errArchivedProject)
		return nil
	}
	body, ok := readComment(w, r)
	if !ok {
		return nil
	}
	id, err := h.model.createComment(r.Context(), i.ID, userID, body)
	if err != nil {
		return err
	}
	h.notifyMentions(r.Context(), i.ID, userID, nil, body, true)
	return h.respondComment(w, r, http.StatusCreated, id, userID, ws)
}

func (h *Handler) updateComment(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, c, ok, err := h.ownComment(w, r, userID, func(c *Comment) bool { return c.CanEdit })
	if !ok || err != nil {
		return err
	}
	body, ok := readComment(w, r)
	if !ok {
		return nil
	}
	old, err := h.model.updateComment(r.Context(), c.ID, body)
	if err != nil {
		return err
	}
	h.notifyMentions(r.Context(), c.issueID, userID, old, body, true)
	return h.respondComment(w, r, http.StatusOK, c.ID, userID, ws)
}

func (h *Handler) deleteComment(w http.ResponseWriter, r *http.Request, userID string) error {
	_, c, ok, err := h.ownComment(w, r, userID, func(c *Comment) bool { return c.CanDelete })
	if !ok || err != nil {
		return err
	}
	if err := h.model.deleteComment(r.Context(), c.ID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// ownComment returns the path's workspace and comment when the caller may change it (can reports that), or writes
// a 404 or 403 and returns ok=false.
func (h *Handler) ownComment(w http.ResponseWriter, r *http.Request, userID string, can func(*Comment) bool) (*workspace.Workspace, *Comment, bool, error) {
	ws, ok, err := h.access(w, r, userID, "")
	if !ok || err != nil {
		return nil, nil, false, err
	}
	c, err := h.model.comment(r.Context(), ws.ID, r.PathValue("id"))
	switch {
	case err != nil:
		return nil, nil, false, err
	case c == nil:
		writeErr(w, http.StatusNotFound, errCommentNotFound)
		return nil, nil, false, nil
	case c.archived:
		writeErr(w, http.StatusForbidden, errArchivedProject)
		return nil, nil, false, nil
	}
	if allowComment(c, userID, ws.Permissions); !can(c) {
		writeErr(w, http.StatusForbidden, errForbidden)
		return nil, nil, false, nil
	}
	return ws, c, true, nil
}

func (h *Handler) respondComment(w http.ResponseWriter, r *http.Request, status int, id, userID string, ws *workspace.Workspace) error {
	c, err := h.model.comment(r.Context(), ws.ID, id)
	if err != nil {
		return err
	}
	if c == nil {
		writeErr(w, http.StatusNotFound, errCommentNotFound)
		return nil
	}
	allowComment(c, userID, ws.Permissions)
	writeJSON(w, status, c)
	return nil
}

// allowComment sets the caller's can_edit and can_delete: comments.manage, or they wrote it (and editing still needs
// comments.create). Nothing in an archived project.
func allowComment(c *Comment, userID string, perms []string) {
	manage := workspace.Has(perms, workspace.PermCommentsManage)
	author := c.Author != nil && c.Author.ID == userID
	c.CanEdit = !c.archived && (manage || author && workspace.Has(perms, workspace.PermCommentsCreate))
	c.CanDelete = !c.archived && (manage || author)
}

// readComment returns the form's checked body, or writes a 400 and returns ok=false.
func readComment(w http.ResponseWriter, r *http.Request) (json.RawMessage, bool) {
	_ = r.ParseMultipartForm(maxBody)
	body, msg := checkComment(r.PostForm.Get("body"))
	return body, checkFields(w, map[string]string{"body": msg})
}

// --- statuses ---

func (h *Handler) listStatuses(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, "")
	if !ok || err != nil {
		return err
	}
	ss, err := h.model.listStatuses(r.Context(), ws.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, ss)
	return nil
}

func (h *Handler) createStatus(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, workspace.PermWorkflowManage)
	if !ok || err != nil {
		return err
	}
	name, category, ok := readStatus(w, r)
	if !ok {
		return nil
	}
	s, err := h.model.createStatus(r.Context(), ws.ID, name, category)
	if errors.Is(err, errNameTaken) {
		writeFields(w, http.StatusConflict, map[string]string{"name": "A status with that name already exists."})
		return nil
	}
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, s)
	return nil
}

func (h *Handler) updateStatus(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, workspace.PermWorkflowManage)
	if !ok || err != nil {
		return err
	}
	name, category, ok := readStatus(w, r)
	if !ok {
		return nil
	}
	s, err := h.model.updateStatus(r.Context(), ws.ID, r.PathValue("id"), name, category)
	switch {
	case errors.Is(err, errNameTaken):
		writeFields(w, http.StatusConflict, map[string]string{"name": "A status with that name already exists."})
	case err != nil:
		return err
	case s == nil:
		writeErr(w, http.StatusNotFound, errStatusNotFound)
	default:
		writeJSON(w, http.StatusOK, s)
	}
	return nil
}

func (h *Handler) orderStatuses(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, workspace.PermWorkflowManage)
	if !ok || err != nil {
		return err
	}
	_ = r.ParseMultipartForm(maxBody)
	err = h.model.reorderStatuses(r.Context(), ws.ID, r.PostForm["ids"])
	if errors.Is(err, errBadOrder) {
		writeErr(w, http.StatusBadRequest, "Send every status once.")
		return nil
	}
	if err != nil {
		return err
	}
	ss, err := h.model.listStatuses(r.Context(), ws.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, ss)
	return nil
}

func (h *Handler) deleteStatus(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, workspace.PermWorkflowManage)
	if !ok || err != nil {
		return err
	}
	found, err := h.model.deleteStatus(r.Context(), ws.ID, r.PathValue("id"), r.URL.Query().Get("move_to"))
	switch {
	case errors.Is(err, errLastStatus):
		writeErr(w, http.StatusConflict, "A workspace needs at least one status.")
	case errors.Is(err, errInUse):
		writeErr(w, http.StatusConflict, "Move its issues to another status first.")
	case err != nil:
		return err
	case !found:
		writeErr(w, http.StatusNotFound, errStatusNotFound)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
	return nil
}

// readStatus returns the trimmed name and category, or writes a 400 with field errors and returns ok=false.
func readStatus(w http.ResponseWriter, r *http.Request) (name, category string, ok bool) {
	name, category = strings.TrimSpace(r.FormValue("name")), r.FormValue("category")
	return name, category, checkFields(w, map[string]string{"name": checkStatusName(name), "category": checkCategory(category)})
}

// --- labels ---

func (h *Handler) listLabels(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, "")
	if !ok || err != nil {
		return err
	}
	ls, err := h.model.listLabels(r.Context(), ws.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, ls)
	return nil
}

func (h *Handler) createLabel(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, workspace.PermWorkflowManage)
	if !ok || err != nil {
		return err
	}
	name, color, ok := readLabel(w, r)
	if !ok {
		return nil
	}
	l, err := h.model.createLabel(r.Context(), ws.ID, name, color)
	if errors.Is(err, errNameTaken) {
		writeFields(w, http.StatusConflict, map[string]string{"name": "A label with that name already exists."})
		return nil
	}
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, l)
	return nil
}

func (h *Handler) updateLabel(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, workspace.PermWorkflowManage)
	if !ok || err != nil {
		return err
	}
	name, color, ok := readLabel(w, r)
	if !ok {
		return nil
	}
	l, err := h.model.updateLabel(r.Context(), ws.ID, r.PathValue("id"), name, color)
	switch {
	case errors.Is(err, errNameTaken):
		writeFields(w, http.StatusConflict, map[string]string{"name": "A label with that name already exists."})
	case err != nil:
		return err
	case l == nil:
		writeErr(w, http.StatusNotFound, errLabelNotFound)
	default:
		writeJSON(w, http.StatusOK, l)
	}
	return nil
}

func (h *Handler) deleteLabel(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, workspace.PermWorkflowManage)
	if !ok || err != nil {
		return err
	}
	found, err := h.model.deleteLabel(r.Context(), ws.ID, r.PathValue("id"))
	if err != nil {
		return err
	}
	if !found {
		writeErr(w, http.StatusNotFound, errLabelNotFound)
		return nil
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// readLabel returns the trimmed name and normalized color, or writes a 400 with field errors and returns ok=false.
func readLabel(w http.ResponseWriter, r *http.Request) (name, color string, ok bool) {
	name, color = strings.TrimSpace(r.FormValue("name")), normColor(r.FormValue("color"))
	return name, color, checkFields(w, map[string]string{"name": checkLabelName(name), "color": checkColor(color)})
}

// --- shared ---

// access returns the path's workspace. It writes a 404 when the caller isn't a member, or a 403 when perm is set
// and they lack it, and returns ok=false.
func (h *Handler) access(w http.ResponseWriter, r *http.Request, userID, perm string) (*workspace.Workspace, bool, error) {
	ws, err := h.member(r.Context(), userID, r.PathValue("slug"))
	switch {
	case err != nil:
		return nil, false, err
	case ws == nil:
		writeErr(w, http.StatusNotFound, errNotFound)
		return nil, false, nil
	case perm != "" && !workspace.Has(ws.Permissions, perm):
		writeErr(w, http.StatusForbidden, errForbidden)
		return nil, false, nil
	}
	return ws, true, nil
}

// checkFields drops empty messages and writes a 400 with the rest, returning false when there were any.
func checkFields(w http.ResponseWriter, msgs map[string]string) bool {
	maps.DeleteFunc(msgs, func(_, msg string) bool { return msg == "" })
	if len(msgs) > 0 {
		writeFields(w, http.StatusBadRequest, msgs)
		return false
	}
	return true
}

func normKey(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

// --- responses ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeFields(w http.ResponseWriter, status int, errs map[string]string) {
	writeJSON(w, status, map[string]map[string]string{"errors": errs})
}
