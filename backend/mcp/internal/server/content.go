package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// contentTools registers the tools for notes, saved views, templates and customer feedback.
func (t *tools) contentTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "list_notes", Annotations: readOnly,
		Description: "List your notes (yours and the ones shared with you), pinned first. Archived notes are left out unless archived is true."}, t.listNotes)
	mcp.AddTool(s, &mcp.Tool{Name: "get_note", Annotations: readOnly, Description: "Read one note by its id from list_notes."}, t.getNote)
	mcp.AddTool(s, &mcp.Tool{Name: "create_note", Description: "Create a note with a title, a body or both."}, t.createNote)
	mcp.AddTool(s, &mcp.Tool{Name: "update_note",
		Description: "Change a note. Only the fields you give change. Pin, archive and labels are your own; title, body and color " +
			"are everyone's on the note and need edit access."}, t.updateNote)
	mcp.AddTool(s, &mcp.Tool{Name: "share_note",
		Description: "Share a note with a workspace member, or change their role on it: view or edit."}, t.shareNote)
	mcp.AddTool(s, &mcp.Tool{Name: "unshare_note",
		Description: "Take a member off a note. The owner may remove anyone; others only themselves."}, t.unshareNote)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_note", Annotations: destructive,
		Description: "Delete a note you own, for everyone. On a note shared with you it only removes it from your notes."}, t.deleteNote)

	mcp.AddTool(s, &mcp.Tool{Name: "list_views", Annotations: readOnly,
		Description: "List the saved views you see: your own and the workspace's shared ones."}, t.listViews)
	mcp.AddTool(s, &mcp.Tool{Name: "create_view",
		Description: "Save an issue page's filters and display under a name. The path is /my-issues, /projects/ENG or " +
			"/projects/ENG/cycles/3, with any query such as ?view=board."}, t.createView)
	mcp.AddTool(s, &mcp.Tool{Name: "update_view", Description: "Rename a saved view, change its path or share it. Only the fields you give change."}, t.updateView)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_view", Annotations: destructive, Description: "Delete a saved view."}, t.deleteView)

	mcp.AddTool(s, &mcp.Tool{Name: "list_templates", Annotations: readOnly,
		Description: "List the workspace's issue templates, with what they prefill and how they repeat."}, t.listTemplates)
	mcp.AddTool(s, &mcp.Tool{Name: "create_template",
		Description: "Create an issue template. Status, assignee and labels are names from workspace_options. To make it repeat " +
			"(creating an issue in its project on schedule), give title, project, repeat_every, repeat_unit and starts_on."}, t.createTemplate)
	mcp.AddTool(s, &mcp.Tool{Name: "update_template",
		Description: "Change an issue template. Only the fields you give change; empty clears a field, repeat_every 0 stops the repeat."}, t.updateTemplate)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_template", Annotations: destructive, Description: "Delete an issue template."}, t.deleteTemplate)

	mcp.AddTool(s, &mcp.Tool{Name: "list_feedback", Annotations: readOnly,
		Description: "List customer feedback, newest first, 50 at a time: new (the default), linked to an issue, or archived. " +
			"With issue, lists the feedback linked to that issue instead."}, t.listFeedback)
	mcp.AddTool(s, &mcp.Tool{Name: "add_feedback", Description: "Record customer feedback, optionally with the customer's email and name."}, t.addFeedback)
	mcp.AddTool(s, &mcp.Tool{Name: "update_feedback",
		Description: "Link feedback to an issue (an empty issue unlinks it), or archive and restore it."}, t.updateFeedback)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_feedback", Annotations: destructive, Description: "Delete customer feedback."}, t.deleteFeedback)
}

// --- notes ---

type noteT struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	Body      json.RawMessage `json:"body"`
	Color     string          `json:"color"`
	Owner     person          `json:"owner"`
	Members   []noteMemberT   `json:"members"`
	Pinned    bool            `json:"pinned"`
	Archived  bool            `json:"archived"`
	Labels    []string        `json:"labels"`
	CanEdit   bool            `json:"can_edit"`
	UpdatedAt string          `json:"updated_at"`
}

// noteMemberT is someone the note is shared with, as the API answers.
type noteMemberT struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

type noteMember struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

type noteOut struct {
	ID         string       `json:"id"`
	Title      string       `json:"title,omitempty"`
	Text       string       `json:"text,omitempty"`
	Color      string       `json:"color,omitempty"`
	Owner      string       `json:"owner"`
	SharedWith []noteMember `json:"shared_with,omitempty"`
	Pinned     bool         `json:"pinned,omitempty"`
	Archived   bool         `json:"archived,omitempty"`
	Labels     []string     `json:"labels,omitempty"`
	CanEdit    bool         `json:"can_edit"`
	UpdatedAt  string       `json:"updated_at"`
	URL        string       `json:"url"`
}

type listNotesIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Label     string `json:"label,omitempty" jsonschema:"only notes with this label"`
	Pinned    *bool  `json:"pinned,omitempty" jsonschema:"true for pinned notes only, false for unpinned only"`
	Archived  bool   `json:"archived,omitempty" jsonschema:"true lists archived notes instead"`
	Query     string `json:"query,omitempty" jsonschema:"words in the title or body"`
}

type noteIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	ID        string `json:"id" jsonschema:"note id, from list_notes"`
}

type createNoteIn struct {
	Workspace string   `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Title     string   `json:"title,omitempty"`
	Body      string   `json:"body,omitempty" jsonschema:"plain text; blank lines separate paragraphs"`
	Color     string   `json:"color,omitempty" jsonschema:"#rrggbb; none when left out"`
	Pinned    bool     `json:"pinned,omitempty"`
	Labels    []string `json:"labels,omitempty" jsonschema:"your labels for it, up to 20"`
}

type updateNoteIn struct {
	Workspace string   `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	ID        string   `json:"id" jsonschema:"note id, from list_notes"`
	Title     *string  `json:"title,omitempty"`
	Body      *string  `json:"body,omitempty" jsonschema:"plain text; blank lines separate paragraphs; empty clears it"`
	Color     *string  `json:"color,omitempty" jsonschema:"#rrggbb; empty for none"`
	Pinned    *bool    `json:"pinned,omitempty"`
	Archived  *bool    `json:"archived,omitempty" jsonschema:"archiving also unpins"`
	Labels    []string `json:"labels,omitempty" jsonschema:"your labels, replacing them all; [] removes them"`
}

type shareNoteIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	ID        string `json:"id" jsonschema:"note id, from list_notes"`
	Member    string `json:"member" jsonschema:"member's email or name"`
	Role      string `json:"role,omitempty" jsonschema:"view or edit; edit when left out"`
}

func (t *tools) noteOut(slug string, n noteT) noteOut {
	o := noteOut{ID: n.ID, Title: n.Title, Text: plainText(n.Body), Color: n.Color, Owner: n.Owner.Name, Pinned: n.Pinned,
		Archived: n.Archived, Labels: n.Labels, CanEdit: n.CanEdit, UpdatedAt: n.UpdatedAt,
		URL: t.app + "/" + url.PathEscape(slug) + "/notes?note=" + url.QueryEscape(n.ID)}
	for _, m := range n.Members {
		o.SharedWith = append(o.SharedWith, noteMember{Name: m.Name, Role: m.Role})
	}
	return o
}

func (t *tools) notes(ctx context.Context, slug string) ([]noteT, error) {
	var ns []noteT
	return ns, t.api.get(ctx, ws(slug, "notes"), nil, &ns)
}

// note is the caller's note with the id; the API has no single-note GET.
func (t *tools) note(ctx context.Context, slug, id string) (noteT, error) {
	ns, err := t.notes(ctx, slug)
	if err != nil {
		return noteT{}, err
	}
	if k := slices.IndexFunc(ns, func(n noteT) bool { return n.ID == strings.TrimSpace(id) }); k >= 0 {
		return ns[k], nil
	}
	return noteT{}, errors.New("Note not found.")
}

func (t *tools) listNotes(ctx context.Context, _ *mcp.CallToolRequest, in listNotesIn) (*mcp.CallToolResult, any, error) {
	ns, err := t.notes(ctx, in.Workspace)
	if err != nil {
		return nil, nil, err
	}
	label, q := strings.TrimSpace(in.Label), strings.ToLower(strings.TrimSpace(in.Query))
	out := []noteOut{}
	for _, n := range ns {
		o := t.noteOut(in.Workspace, n)
		if n.Archived != in.Archived || in.Pinned != nil && n.Pinned != *in.Pinned ||
			label != "" && !slices.ContainsFunc(n.Labels, func(l string) bool { return strings.EqualFold(l, label) }) ||
			q != "" && !strings.Contains(strings.ToLower(o.Title+"\n"+o.Text), q) {
			continue
		}
		out = append(out, o)
	}
	return result(out)
}

func (t *tools) getNote(ctx context.Context, _ *mcp.CallToolRequest, in noteIn) (*mcp.CallToolResult, any, error) {
	n, err := t.note(ctx, in.Workspace, in.ID)
	if err != nil {
		return nil, nil, err
	}
	return result(t.noteOut(in.Workspace, n))
}

func (t *tools) createNote(ctx context.Context, _ *mcp.CallToolRequest, in createNoteIn) (*mcp.CallToolResult, any, error) {
	form := url.Values{"title": {strings.TrimSpace(in.Title)}, "color": {strings.TrimSpace(in.Color)}}
	if strings.TrimSpace(in.Body) != "" {
		form.Set("body", toDoc(in.Body))
	}
	if in.Pinned {
		form.Set("pinned", "1")
	}
	if in.Labels != nil {
		b, _ := json.Marshal(in.Labels) // a []string always marshals
		form.Set("labels", string(b))
	}
	var n noteT
	if err := t.api.post(ctx, ws(in.Workspace, "notes"), form, &n); err != nil {
		return nil, nil, err
	}
	return result(t.noteOut(in.Workspace, n))
}

func (t *tools) updateNote(ctx context.Context, _ *mcp.CallToolRequest, in updateNoteIn) (*mcp.CallToolResult, any, error) {
	form := url.Values{}
	if in.Title != nil {
		form.Set("title", strings.TrimSpace(*in.Title))
	}
	if in.Body != nil {
		form.Set("body", "")
		if strings.TrimSpace(*in.Body) != "" {
			form.Set("body", toDoc(*in.Body))
		}
	}
	if in.Color != nil {
		form.Set("color", strings.TrimSpace(*in.Color))
	}
	for k, v := range map[string]*bool{"pinned": in.Pinned, "archived": in.Archived} {
		if v != nil {
			form.Set(k, formBool(*v))
		}
	}
	if in.Labels != nil {
		b, _ := json.Marshal(in.Labels) // a []string always marshals
		form.Set("labels", string(b))
	}
	if len(form) == 0 {
		return nil, nil, errors.New("Give at least one field to change.")
	}
	var n noteT
	if err := t.api.post(ctx, ws(in.Workspace, "notes", in.ID), form, &n); err != nil {
		return nil, nil, err
	}
	return result(t.noteOut(in.Workspace, n))
}

func (t *tools) shareNote(ctx context.Context, _ *mcp.CallToolRequest, in shareNoteIn) (*mcp.CallToolResult, any, error) {
	m, err := t.member(ctx, in.Workspace, in.Member)
	if err != nil {
		return nil, nil, err
	}
	n, err := t.note(ctx, in.Workspace, in.ID)
	if err != nil {
		return nil, nil, err
	}
	role := strings.ToLower(strings.TrimSpace(in.Role))
	if role == "" {
		role = "edit"
	}
	path, form := ws(in.Workspace, "notes", n.ID, "members"), url.Values{"user_id": {m.ID}, "role": {role}}
	if slices.ContainsFunc(n.Members, func(x noteMemberT) bool { return x.ID == m.ID }) {
		path, form = ws(in.Workspace, "notes", n.ID, "members", m.ID), url.Values{"role": {role}}
	}
	if err := t.api.post(ctx, path, form, &n); err != nil {
		return nil, nil, err
	}
	return result(t.noteOut(in.Workspace, n))
}

func (t *tools) unshareNote(ctx context.Context, _ *mcp.CallToolRequest, in shareNoteIn) (*mcp.CallToolResult, any, error) {
	m, err := t.member(ctx, in.Workspace, in.Member)
	if err != nil {
		return nil, nil, err
	}
	if err := t.api.del(ctx, ws(in.Workspace, "notes", in.ID, "members", m.ID), nil); err != nil {
		return nil, nil, err
	}
	return result(map[string]string{"removed": m.Name})
}

func (t *tools) deleteNote(ctx context.Context, _ *mcp.CallToolRequest, in noteIn) (*mcp.CallToolResult, any, error) {
	if err := t.api.del(ctx, ws(in.Workspace, "notes", in.ID), nil); err != nil {
		return nil, nil, err
	}
	return result(map[string]string{"deleted": in.ID})
}

// --- saved views ---

type viewT struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Shared  bool   `json:"shared"`
	Owner   person `json:"owner"`
	CanEdit bool   `json:"can_edit"`
}

type viewOut struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Shared  bool   `json:"shared"`
	Owner   string `json:"owner"`
	CanEdit bool   `json:"can_edit"`
	URL     string `json:"url"`
}

type createViewIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Name      string `json:"name"`
	Path      string `json:"path" jsonschema:"such as /projects/ENG?view=board, /projects/ENG/cycles/3 or /my-issues"`
	Shared    bool   `json:"shared,omitempty" jsonschema:"true shows it to the whole workspace"`
}

type updateViewIn struct {
	Workspace string  `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	ID        string  `json:"id" jsonschema:"view id, from list_views"`
	Name      *string `json:"name,omitempty"`
	Path      *string `json:"path,omitempty" jsonschema:"such as /projects/ENG?view=board"`
	Shared    *bool   `json:"shared,omitempty"`
}

type viewIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	ID        string `json:"id" jsonschema:"view id, from list_views"`
}

func (t *tools) viewOut(slug string, v viewT) viewOut {
	return viewOut{ID: v.ID, Name: v.Name, Path: v.Path, Shared: v.Shared, Owner: v.Owner.Name, CanEdit: v.CanEdit,
		URL: t.app + "/" + url.PathEscape(slug) + v.Path}
}

// viewPath is the path relative to the workspace, also when given as the page's full app link.
func (t *tools) viewPath(slug, p string) string {
	return strings.TrimPrefix(strings.TrimSpace(p), t.app+"/"+url.PathEscape(slug))
}

func (t *tools) listViews(ctx context.Context, _ *mcp.CallToolRequest, in wsIn) (*mcp.CallToolResult, any, error) {
	var vs []viewT
	if err := t.api.get(ctx, ws(in.Workspace, "views"), nil, &vs); err != nil {
		return nil, nil, err
	}
	out := make([]viewOut, len(vs))
	for k, v := range vs {
		out[k] = t.viewOut(in.Workspace, v)
	}
	return result(out)
}

func (t *tools) createView(ctx context.Context, _ *mcp.CallToolRequest, in createViewIn) (*mcp.CallToolResult, any, error) {
	form := url.Values{"name": {in.Name}, "path": {t.viewPath(in.Workspace, in.Path)}, "shared": {formBool(in.Shared)}}
	var v viewT
	if err := t.api.post(ctx, ws(in.Workspace, "views"), form, &v); err != nil {
		return nil, nil, err
	}
	return result(t.viewOut(in.Workspace, v))
}

func (t *tools) updateView(ctx context.Context, _ *mcp.CallToolRequest, in updateViewIn) (*mcp.CallToolResult, any, error) {
	form := url.Values{}
	if in.Name != nil {
		form.Set("name", *in.Name)
	}
	if in.Path != nil {
		form.Set("path", t.viewPath(in.Workspace, *in.Path))
	}
	if in.Shared != nil {
		form.Set("shared", formBool(*in.Shared))
	}
	if len(form) == 0 {
		return nil, nil, errors.New("Give at least one field to change.")
	}
	var v viewT
	if err := t.api.post(ctx, ws(in.Workspace, "views", in.ID), form, &v); err != nil {
		return nil, nil, err
	}
	return result(t.viewOut(in.Workspace, v))
}

func (t *tools) deleteView(ctx context.Context, _ *mcp.CallToolRequest, in viewIn) (*mcp.CallToolResult, any, error) {
	if err := t.api.del(ctx, ws(in.Workspace, "views", in.ID), nil); err != nil {
		return nil, nil, err
	}
	return result(map[string]string{"deleted": in.ID})
}

// --- templates ---

type templateT struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Title       string          `json:"title"`
	Description json.RawMessage `json:"description"`
	Project     *project        `json:"project"`
	Status      *status         `json:"status"`
	Assignee    *person         `json:"assignee"`
	Priority    int             `json:"priority"`
	Labels      []label         `json:"labels"`
	Repeat      *struct {
		Every  int    `json:"every"`
		Unit   string `json:"unit"`
		NextOn string `json:"next_on"`
		Paused bool   `json:"paused"`
	} `json:"repeat"`
	CanEdit bool `json:"can_edit"`
}

type templateOut struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Project     string   `json:"project,omitempty"`
	Status      string   `json:"status,omitempty"`
	Assignee    string   `json:"assignee,omitempty"`
	Priority    string   `json:"priority,omitempty"`
	Labels      []string `json:"labels,omitempty"`
	Repeat      string   `json:"repeat,omitempty"`
	CanEdit     bool     `json:"can_edit"`
}

// templateFields are the fields create_template and update_template take; nil leaves one unchanged.
type templateFields struct {
	Title       *string  `json:"title,omitempty" jsonschema:"the new issue's title"`
	Description *string  `json:"description,omitempty" jsonschema:"plain text; blank lines separate paragraphs"`
	Project     *string  `json:"project,omitempty" jsonschema:"project key, such as ENG"`
	Status      *string  `json:"status,omitempty" jsonschema:"status name"`
	Assignee    *string  `json:"assignee,omitempty" jsonschema:"member's email or name"`
	Priority    *string  `json:"priority,omitempty" jsonschema:"none, urgent, high, medium or low"`
	Labels      []string `json:"labels,omitempty" jsonschema:"label names, replacing the template's labels"`
	RepeatEvery *int     `json:"repeat_every,omitempty" jsonschema:"1 to 365, with repeat_unit and starts_on; 0 stops the repeat"`
	RepeatUnit  *string  `json:"repeat_unit,omitempty" jsonschema:"day, week or month"`
	StartsOn    *string  `json:"starts_on,omitempty" jsonschema:"YYYY-MM-DD of the first issue, today or later"`
	Paused      *bool    `json:"paused,omitempty" jsonschema:"true pauses the repeat"`
}

type createTemplateIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Name      string `json:"name"`
	templateFields
}

type updateTemplateIn struct {
	Workspace string  `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	ID        string  `json:"id" jsonschema:"template id, from list_templates"`
	Name      *string `json:"name,omitempty"`
	templateFields
}

type templateIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	ID        string `json:"id" jsonschema:"template id, from list_templates"`
}

func templateOutOf(tp templateT) templateOut {
	o := templateOut{ID: tp.ID, Name: tp.Name, Title: tp.Title, Description: plainText(tp.Description),
		Assignee: name(tp.Assignee), CanEdit: tp.CanEdit}
	if tp.Project != nil {
		o.Project = tp.Project.Key
	}
	if tp.Status != nil {
		o.Status = tp.Status.Name
	}
	if tp.Priority > 0 && tp.Priority < len(priorities) {
		o.Priority = priorities[tp.Priority]
	}
	for _, l := range tp.Labels {
		o.Labels = append(o.Labels, l.Name)
	}
	if r := tp.Repeat; r != nil {
		o.Repeat = fmt.Sprintf("every %d %s, next on %s", r.Every, r.Unit, r.NextOn)
		if r.Paused {
			o.Repeat += " (paused)"
		}
	}
	return o
}

// templateForm is the API form for the fields given, names resolved to ids the way issues take them.
func (t *tools) templateForm(ctx context.Context, slug string, name *string, f templateFields) (url.Values, error) {
	form, err := t.issueForm(ctx, slug, "", fields{title: f.Title, description: f.Description, status: f.Status,
		assignee: f.Assignee, priority: f.Priority, labels: f.Labels})
	if err != nil {
		return nil, err
	}
	if name != nil {
		form.Set("name", strings.TrimSpace(*name))
	}
	if f.Project != nil {
		form.Set("project", strings.TrimSpace(*f.Project))
	}
	if f.RepeatEvery != nil && *f.RepeatEvery == 0 {
		form.Set("repeat_every", "")
		form.Set("repeat_unit", "")
		form.Set("starts_on", "")
	} else if f.RepeatEvery != nil || f.RepeatUnit != nil || f.StartsOn != nil {
		// The API takes the three together.
		form.Set("repeat_every", "")
		if f.RepeatEvery != nil {
			form.Set("repeat_every", strconv.Itoa(*f.RepeatEvery))
		}
		form.Set("repeat_unit", orEmpty(f.RepeatUnit))
		form.Set("starts_on", orEmpty(f.StartsOn))
	}
	if f.Paused != nil {
		form.Set("paused", formBool(*f.Paused))
	}
	return form, nil
}

func (t *tools) listTemplates(ctx context.Context, _ *mcp.CallToolRequest, in wsIn) (*mcp.CallToolResult, any, error) {
	var ts []templateT
	if err := t.api.get(ctx, ws(in.Workspace, "templates"), nil, &ts); err != nil {
		return nil, nil, err
	}
	out := make([]templateOut, len(ts))
	for k, tp := range ts {
		out[k] = templateOutOf(tp)
	}
	return result(out)
}

func (t *tools) createTemplate(ctx context.Context, _ *mcp.CallToolRequest, in createTemplateIn) (*mcp.CallToolResult, any, error) {
	form, err := t.templateForm(ctx, in.Workspace, &in.Name, in.templateFields)
	if err != nil {
		return nil, nil, err
	}
	var tp templateT
	if err := t.api.post(ctx, ws(in.Workspace, "templates"), form, &tp); err != nil {
		return nil, nil, err
	}
	return result(templateOutOf(tp))
}

func (t *tools) updateTemplate(ctx context.Context, _ *mcp.CallToolRequest, in updateTemplateIn) (*mcp.CallToolResult, any, error) {
	form, err := t.templateForm(ctx, in.Workspace, in.Name, in.templateFields)
	if err != nil {
		return nil, nil, err
	}
	if len(form) == 0 {
		return nil, nil, errors.New("Give at least one field to change.")
	}
	var tp templateT
	if err := t.api.post(ctx, ws(in.Workspace, "templates", in.ID), form, &tp); err != nil {
		return nil, nil, err
	}
	return result(templateOutOf(tp))
}

func (t *tools) deleteTemplate(ctx context.Context, _ *mcp.CallToolRequest, in templateIn) (*mcp.CallToolResult, any, error) {
	if err := t.api.del(ctx, ws(in.Workspace, "templates", in.ID), nil); err != nil {
		return nil, nil, err
	}
	return result(map[string]string{"deleted": in.ID})
}

// --- feedback ---

type feedbackT struct {
	ID     string  `json:"id"`
	Body   string  `json:"body"`
	Email  *string `json:"email"`
	Name   *string `json:"name"`
	Source string  `json:"source"`
	Issue  *struct {
		Identifier string `json:"identifier"`
		Title      string `json:"title"`
	} `json:"issue"`
	Archived  bool    `json:"archived"`
	CreatedBy *person `json:"created_by"`
	CreatedAt string  `json:"created_at"`
}

type feedbackOut struct {
	ID        string `json:"id"`
	Body      string `json:"body"`
	Email     string `json:"email,omitempty"`
	Name      string `json:"name,omitempty"`
	Source    string `json:"source"`
	Issue     string `json:"issue,omitempty"`
	Archived  bool   `json:"archived,omitempty"`
	AddedBy   string `json:"added_by,omitempty"`
	CreatedAt string `json:"created_at"`
}

type listFeedbackIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	State     string `json:"state,omitempty" jsonschema:"new (the default), linked or archived"`
	Query     string `json:"query,omitempty" jsonschema:"words in the feedback, email or name"`
	Issue     string `json:"issue,omitempty" jsonschema:"issue identifier, such as ENG-142, to list its linked feedback"`
	Before    string `json:"before,omitempty" jsonschema:"the last id of the previous page, when has_more"`
}

type addFeedbackIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Body      string `json:"body" jsonschema:"what the customer said, up to 5,000 characters"`
	Email     string `json:"email,omitempty" jsonschema:"the customer's email"`
	Name      string `json:"name,omitempty" jsonschema:"the customer's name"`
}

type updateFeedbackIn struct {
	Workspace string  `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	ID        string  `json:"id" jsonschema:"feedback id, from list_feedback"`
	Issue     *string `json:"issue,omitempty" jsonschema:"issue identifier to link it to, such as ENG-142; empty unlinks it"`
	Archived  *bool   `json:"archived,omitempty"`
}

type feedbackIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	ID        string `json:"id" jsonschema:"feedback id, from list_feedback"`
}

func feedbackOutOf(f feedbackT) feedbackOut {
	o := feedbackOut{ID: f.ID, Body: f.Body, Email: orEmpty(f.Email), Name: orEmpty(f.Name), Source: f.Source, Archived: f.Archived,
		AddedBy: name(f.CreatedBy), CreatedAt: f.CreatedAt}
	if f.Issue != nil {
		o.Issue = f.Issue.Identifier + " " + f.Issue.Title
	}
	return o
}

func (t *tools) listFeedback(ctx context.Context, _ *mcp.CallToolRequest, in listFeedbackIn) (*mcp.CallToolResult, any, error) {
	q := url.Values{}
	if in.Before != "" {
		q.Set("before", strings.TrimSpace(in.Before))
	}
	path := ws(in.Workspace, "feedback")
	if in.Issue != "" {
		path = ws(in.Workspace, "issues", strings.TrimSpace(in.Issue), "feedback")
	} else {
		if s := strings.ToLower(strings.TrimSpace(in.State)); s != "" {
			if !slices.Contains([]string{"new", "linked", "archived"}, s) {
				return nil, nil, fmt.Errorf("No state %q. Choose one of: new, linked, archived.", in.State)
			}
			q.Set("state", s)
		}
		if s := strings.TrimSpace(in.Query); s != "" {
			q.Set("q", s)
		}
	}
	var res struct {
		Items   []feedbackT `json:"items"`
		HasMore bool        `json:"has_more"`
	}
	if err := t.api.get(ctx, path, q, &res); err != nil {
		return nil, nil, err
	}
	out := struct {
		Items   []feedbackOut `json:"items"`
		HasMore bool          `json:"has_more"`
	}{Items: make([]feedbackOut, len(res.Items)), HasMore: res.HasMore}
	for k, f := range res.Items {
		out.Items[k] = feedbackOutOf(f)
	}
	return result(out)
}

func (t *tools) addFeedback(ctx context.Context, _ *mcp.CallToolRequest, in addFeedbackIn) (*mcp.CallToolResult, any, error) {
	var f feedbackT
	form := url.Values{"body": {in.Body}, "email": {in.Email}, "name": {in.Name}}
	if err := t.api.post(ctx, ws(in.Workspace, "feedback"), form, &f); err != nil {
		return nil, nil, err
	}
	return result(feedbackOutOf(f))
}

func (t *tools) updateFeedback(ctx context.Context, _ *mcp.CallToolRequest, in updateFeedbackIn) (*mcp.CallToolResult, any, error) {
	form := url.Values{}
	if in.Issue != nil {
		form.Set("issue", strings.TrimSpace(*in.Issue))
	}
	if in.Archived != nil {
		form.Set("archived", strconv.FormatBool(*in.Archived))
	}
	if len(form) == 0 {
		return nil, nil, errors.New("Give issue or archived.")
	}
	var f feedbackT
	if err := t.api.post(ctx, ws(in.Workspace, "feedback", in.ID), form, &f); err != nil {
		return nil, nil, err
	}
	return result(feedbackOutOf(f))
}

func (t *tools) deleteFeedback(ctx context.Context, _ *mcp.CallToolRequest, in feedbackIn) (*mcp.CallToolResult, any, error) {
	if err := t.api.del(ctx, ws(in.Workspace, "feedback", in.ID), nil); err != nil {
		return nil, nil, err
	}
	return result(map[string]string{"deleted": in.ID})
}

// formBool is the API's "1" or "0" for a boolean field.
func formBool(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func orEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
