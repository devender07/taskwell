// Package server is Taskwell's MCP server: tools that read and change issues by calling the user service's REST API
// with the caller's personal API token (docs/mcp-plan.md).
//
// server.go registers the tools, api.go is the REST client and text.go converts descriptions and comments.
package server

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Handler serves /mcp: stateless streamable HTTP, a server per request whose tools call apiURL with that request's
// Authorization header. A request without a personal API token gets 401 before reaching the SDK, pointing OAuth
// clients at the protected resource metadata (see Metadata).
func Handler(apiURL, appURL, mcpURL string, client *http.Client) http.Handler {
	challenge := `Bearer resource_metadata="` + strings.TrimRight(mcpURL, "/") + `/.well-known/oauth-protected-resource/mcp"`
	cache := mcp.NewSchemaCache()
	h := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		t := &tools{api: &api{base: strings.TrimRight(apiURL, "/"), auth: r.Header.Get("Authorization"), client: client},
			app: strings.TrimRight(appURL, "/")}
		return t.server(cache)
	}, &mcp.StreamableHTTPOptions{Stateless: true})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, tok, _ := strings.Cut(r.Header.Get("Authorization"), " ")
		if !strings.EqualFold(scheme, "Bearer") || !strings.HasPrefix(strings.TrimSpace(tok), "tth_") {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("WWW-Authenticate", challenge)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"Send a Taskwell API token: Authorization: Bearer tth_…"}`))
			return
		}
		h.ServeHTTP(w, r)
	})
}

// Metadata serves the RFC 9728 protected resource metadata for /mcp at mcpURL: the user API at authURL is its
// authorization server.
func Metadata(mcpURL, authURL string) http.HandlerFunc {
	body, _ := json.Marshal(map[string]any{"resource": strings.TrimRight(mcpURL, "/") + "/mcp",
		"authorization_servers":    []string{strings.TrimRight(authURL, "/")},
		"bearer_methods_supported": []string{"header"}, "scopes_supported": []string{"taskwell"}})
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}
}

// tools holds one request's API client; every tool is a method.
type tools struct {
	api *api
	app string // APP_URL, for issue links
}

var readOnly = &mcp.ToolAnnotations{ReadOnlyHint: true}

func (t *tools) server(cache *mcp.SchemaCache) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "taskwell", Title: "Taskwell", Version: "1.0.0"}, &mcp.ServerOptions{SchemaCache: cache})
	mcp.AddTool(s, &mcp.Tool{Name: "list_workspaces", Annotations: readOnly,
		Description: "List the Taskwell workspaces you belong to. Every other tool takes a workspace slug from here."}, t.listWorkspaces)
	mcp.AddTool(s, &mcp.Tool{Name: "list_projects", Annotations: readOnly,
		Description: "List a workspace's active projects with their keys (the ENG in ENG-142)."}, t.listProjects)
	mcp.AddTool(s, &mcp.Tool{Name: "workspace_options", Annotations: readOnly,
		Description: "List a workspace's issue statuses, labels and members: the names other tools accept."}, t.workspaceOptions)
	mcp.AddTool(s, &mcp.Tool{Name: "search_issues", Annotations: readOnly,
		Description: "Find issues by identifier (ENG-142) or words in the title, optionally in one project. Returns up to 20."}, t.searchIssues)
	mcp.AddTool(s, &mcp.Tool{Name: "list_issues", Annotations: readOnly,
		Description: "List a project's issues in its manual order (or sort), optionally filtered by status, assignee, label or priority."}, t.listIssues)
	mcp.AddTool(s, &mcp.Tool{Name: "my_issues", Annotations: readOnly,
		Description: "List the issues assigned to you in a workspace's active projects."}, t.myIssues)
	mcp.AddTool(s, &mcp.Tool{Name: "watching_issues", Annotations: readOnly,
		Description: "List the issues you watch in a workspace's active projects."}, t.watchingIssues)
	mcp.AddTool(s, &mcp.Tool{Name: "get_issue", Annotations: readOnly,
		Description: "Read one issue: its fields, links, description and latest comments (with ids for update_comment and delete_comment)."}, t.getIssue)
	mcp.AddTool(s, &mcp.Tool{Name: "create_issue",
		Description: "Create an issue in a project. Status, assignee and labels are names from workspace_options. " +
			"To file a bug from a failed test result, use the case's title and put the result's summary and reproduction steps in the description."}, t.createIssue)
	mcp.AddTool(s, &mcp.Tool{Name: "update_issue",
		Description: "Change an issue. Only the fields you give change; an empty assignee, due_date, parent or cycle clears it, estimate 0 clears it, labels replaces all labels."}, t.updateIssue)
	mcp.AddTool(s, &mcp.Tool{Name: "reorder_issues",
		Description: "Change a project's manual issue order (list_issues' default). Give identifiers to put them in that order: the first stays " +
			"where it is and each next one moves right after the one before it. Or give identifier with after (the issue it goes right after) " +
			"or before (the issue it goes right before). Returns the moved issues in order."}, t.reorderIssues)
	mcp.AddTool(s, &mcp.Tool{Name: "add_comment", Description: "Comment on an issue."}, t.addComment)
	t.issueTools(s)
	t.projectTools(s)
	t.testTools(s)
	t.browserTools(s)
	t.appTools(s)
	t.chatTools(s)
	t.fileTools(s)
	t.planningTools(s)
	t.contentTools(s)
	t.adminTools(s)
	return s
}

// --- inputs ---

type wsIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
}

type searchIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Query     string `json:"query" jsonschema:"an identifier such as ENG-142, or words in the title"`
	Project   string `json:"project,omitempty" jsonschema:"project key, to search one project only"`
}

type listIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Project   string `json:"project" jsonschema:"project key, such as ENG"`
	Status    string `json:"status,omitempty" jsonschema:"status name"`
	Assignee  string `json:"assignee,omitempty" jsonschema:"member's email or name"`
	Label     string `json:"label,omitempty" jsonschema:"label name"`
	Priority  string `json:"priority,omitempty" jsonschema:"none, urgent, high, medium or low"`
	Sort      string `json:"sort,omitempty" jsonschema:"manual (the default), priority, updated, created or due"`
}

type reorderIssuesIn struct {
	Workspace   string   `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Project     string   `json:"project" jsonschema:"project key, such as ENG"`
	Identifiers []string `json:"identifiers,omitempty" jsonschema:"issue identifiers in the order they should be; the first stays put"`
	Identifier  string   `json:"identifier,omitempty" jsonschema:"the one issue to move, with after or before"`
	After       string   `json:"after,omitempty" jsonschema:"identifier of the issue it goes right after"`
	Before      string   `json:"before,omitempty" jsonschema:"identifier of the issue it goes right before"`
}

type issueIn struct {
	Workspace  string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Identifier string `json:"identifier" jsonschema:"issue identifier, such as ENG-142"`
}

type createIn struct {
	Workspace   string   `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Project     string   `json:"project" jsonschema:"project key, such as ENG"`
	Title       string   `json:"title"`
	Description *string  `json:"description,omitempty" jsonschema:"plain text; blank lines separate paragraphs"`
	Status      *string  `json:"status,omitempty" jsonschema:"status name; the workspace's default when left out"`
	Assignee    *string  `json:"assignee,omitempty" jsonschema:"member's email or name"`
	Priority    *string  `json:"priority,omitempty" jsonschema:"none, urgent, high, medium or low"`
	DueDate     *string  `json:"due_date,omitempty" jsonschema:"YYYY-MM-DD"`
	Estimate    *int     `json:"estimate,omitempty" jsonschema:"story points: 1, 2, 3, 5 or 8"`
	Labels      []string `json:"labels,omitempty" jsonschema:"label names"`
	Parent      *string  `json:"parent,omitempty" jsonschema:"parent issue identifier, making this a sub-issue"`
	Cycle       *string  `json:"cycle,omitempty" jsonschema:"cycle number in the project, from list_cycles"`
	After       string   `json:"after,omitempty" jsonschema:"identifier of the issue it goes right after in the manual order; last when left out"`
	Before      string   `json:"before,omitempty" jsonschema:"identifier of the issue it goes right before in the manual order"`
}

type updateIn struct {
	Workspace   string   `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Identifier  string   `json:"identifier" jsonschema:"issue identifier, such as ENG-142"`
	Title       *string  `json:"title,omitempty"`
	Description *string  `json:"description,omitempty" jsonschema:"plain text; blank lines separate paragraphs; empty clears it"`
	Status      *string  `json:"status,omitempty" jsonschema:"status name"`
	Assignee    *string  `json:"assignee,omitempty" jsonschema:"member's email or name; empty unassigns"`
	Priority    *string  `json:"priority,omitempty" jsonschema:"none, urgent, high, medium or low"`
	DueDate     *string  `json:"due_date,omitempty" jsonschema:"YYYY-MM-DD; empty clears it"`
	Estimate    *int     `json:"estimate,omitempty" jsonschema:"story points: 1, 2, 3, 5 or 8; 0 clears it"`
	Labels      []string `json:"labels,omitempty" jsonschema:"label names, replacing the issue's labels; [] removes them all"`
	Parent      *string  `json:"parent,omitempty" jsonschema:"parent issue identifier; empty makes it top-level"`
	Cycle       *string  `json:"cycle,omitempty" jsonschema:"cycle number in the issue's project, from list_cycles; empty removes it from its cycle"`
}

type commentIn struct {
	Workspace  string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Identifier string `json:"identifier" jsonschema:"issue identifier, such as ENG-142"`
	Body       string `json:"body" jsonschema:"plain text; blank lines separate paragraphs"`
}

// --- API shapes (the fields the tools use) ---

type status struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
}

type label struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type member struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type person struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// issue is an issue, a list item or a search result; Priority is nil in search results.
type issue struct {
	Identifier  string          `json:"identifier"`
	Title       string          `json:"title"`
	Description json.RawMessage `json:"description"`
	Status      status          `json:"status"`
	Assignee    *person         `json:"assignee"`
	Priority    *int            `json:"priority"`
	DueDate     *string         `json:"due_date"`
	Estimate    *int            `json:"estimate"`
	Labels      []label         `json:"labels"`
	Project     *struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	} `json:"project"`
	Parent *ref `json:"parent"`
	Cycle  *struct {
		Number int `json:"number"`
	} `json:"cycle"`
	CreatedBy *person `json:"created_by"`
	CreatedAt string  `json:"created_at"`
	// Only one issue's answer has these.
	SubIssues []ref `json:"sub_issues"`
	Blocks    []ref `json:"blocks"`
	BlockedBy []ref `json:"blocked_by"`
	Watching  *bool `json:"watching"`
}

type ref struct {
	Identifier string `json:"identifier"`
}

type comment struct {
	ID        string          `json:"id"`
	Author    *person         `json:"author"`
	Body      json.RawMessage `json:"body"`
	CreatedAt string          `json:"created_at"`
}

// --- outputs ---

// issueOut is an issue as the tools answer: names instead of ids, the description as plain text.
type issueOut struct {
	Identifier  string       `json:"identifier"`
	Title       string       `json:"title"`
	Status      string       `json:"status"`
	Assignee    string       `json:"assignee,omitempty"`
	Priority    string       `json:"priority,omitempty"`
	DueDate     string       `json:"due_date,omitempty"`
	Estimate    int          `json:"estimate,omitempty"`
	Labels      []string     `json:"labels,omitempty"`
	Project     string       `json:"project,omitempty"`
	Parent      string       `json:"parent,omitempty"`
	Cycle       int          `json:"cycle,omitempty"`
	SubIssues   []string     `json:"sub_issues,omitempty"`
	Blocks      []string     `json:"blocks,omitempty"`
	BlockedBy   []string     `json:"blocked_by,omitempty"`
	Watching    *bool        `json:"watching,omitempty"`
	CreatedBy   string       `json:"created_by,omitempty"`
	CreatedAt   string       `json:"created_at,omitempty"`
	URL         string       `json:"url"`
	Description string       `json:"description,omitempty"`
	Comments    []commentOut `json:"comments,omitempty"`
}

type commentOut struct {
	ID        string `json:"id"`
	Author    string `json:"author"`
	CreatedAt string `json:"created_at"`
	Text      string `json:"text"`
}

var priorities = []string{"none", "urgent", "high", "medium", "low"} // the API's 0–4

func (t *tools) out(slug string, i issue) issueOut {
	o := issueOut{Identifier: i.Identifier, Title: i.Title, Status: i.Status.Name, CreatedAt: i.CreatedAt,
		Description: plainText(i.Description), URL: t.app + "/" + url.PathEscape(slug) + "/issue/" + url.PathEscape(i.Identifier)}
	if i.Assignee != nil {
		o.Assignee = i.Assignee.Name
	}
	if i.Priority != nil && *i.Priority >= 0 && *i.Priority < len(priorities) {
		o.Priority = priorities[*i.Priority]
	}
	if i.DueDate != nil {
		o.DueDate = *i.DueDate
	}
	if i.Estimate != nil {
		o.Estimate = *i.Estimate
	}
	for _, l := range i.Labels {
		o.Labels = append(o.Labels, l.Name)
	}
	if i.Project != nil {
		o.Project = i.Project.Key + " " + i.Project.Name
	}
	if i.Parent != nil {
		o.Parent = i.Parent.Identifier
	}
	if i.Cycle != nil {
		o.Cycle = i.Cycle.Number
	}
	o.SubIssues, o.Blocks, o.BlockedBy, o.Watching = idents(i.SubIssues), idents(i.Blocks), idents(i.BlockedBy), i.Watching
	if i.CreatedBy != nil {
		o.CreatedBy = i.CreatedBy.Name
	}
	return o
}

// outs is a list of issues without the fields one issue's answer adds.
func (t *tools) outs(slug string, is []issue) []issueOut {
	o := make([]issueOut, len(is))
	for k, i := range is {
		o[k] = t.out(slug, i)
		o[k].Project, o[k].CreatedBy, o[k].CreatedAt = "", "", ""
	}
	return o
}

// result answers with v as compact JSON text.
func result(v any) (*mcp.CallToolResult, any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, nil, fmt.Errorf("encode result: %w", err)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil, nil
}

// --- tools ---

func (t *tools) listWorkspaces(ctx context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	var wss []struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	}
	if err := t.api.get(ctx, "/api/workspaces", nil, &wss); err != nil {
		return nil, nil, err
	}
	return result(wss)
}

func (t *tools) listProjects(ctx context.Context, _ *mcp.CallToolRequest, in wsIn) (*mcp.CallToolResult, any, error) {
	var ps []struct {
		Key         string `json:"key"`
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
	}
	if err := t.api.get(ctx, ws(in.Workspace, "projects"), nil, &ps); err != nil {
		return nil, nil, err
	}
	return result(ps)
}

func (t *tools) workspaceOptions(ctx context.Context, _ *mcp.CallToolRequest, in wsIn) (*mcp.CallToolResult, any, error) {
	ss, err := t.statuses(ctx, in.Workspace)
	if err != nil {
		return nil, nil, err
	}
	ls, err := t.labels(ctx, in.Workspace)
	if err != nil {
		return nil, nil, err
	}
	ms, err := t.members(ctx, in.Workspace)
	if err != nil {
		return nil, nil, err
	}
	type memberOut struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	o := struct {
		Statuses   []string    `json:"statuses"`
		Labels     []string    `json:"labels"`
		Members    []memberOut `json:"members"`
		Priorities []string    `json:"priorities"`
	}{Statuses: []string{}, Labels: []string{}, Members: []memberOut{}, Priorities: priorities}
	for _, s := range ss {
		o.Statuses = append(o.Statuses, s.Name)
	}
	for _, l := range ls {
		o.Labels = append(o.Labels, l.Name)
	}
	for _, m := range ms {
		o.Members = append(o.Members, memberOut{m.Name, m.Email})
	}
	return result(o)
}

func (t *tools) searchIssues(ctx context.Context, _ *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, any, error) {
	var is []issue
	q := url.Values{"q": {in.Query}}
	if in.Project != "" {
		q.Set("project", in.Project)
	}
	if err := t.api.get(ctx, ws(in.Workspace, "issues"), q, &is); err != nil {
		return nil, nil, err
	}
	return result(t.outs(in.Workspace, is))
}

func (t *tools) listIssues(ctx context.Context, _ *mcp.CallToolRequest, in listIn) (*mcp.CallToolResult, any, error) {
	// The API has no filters, so they are resolved by name here and applied to the project's list.
	keep := []func(issue) bool{}
	if in.Status != "" {
		ss, err := t.statuses(ctx, in.Workspace)
		if err != nil {
			return nil, nil, err
		}
		s, err := find("status", in.Status, ss, func(s status) []string { return []string{s.Name} }, statusName)
		if err != nil {
			return nil, nil, err
		}
		keep = append(keep, func(i issue) bool { return i.Status.ID == s.ID })
	}
	if in.Assignee != "" {
		m, err := t.member(ctx, in.Workspace, in.Assignee)
		if err != nil {
			return nil, nil, err
		}
		keep = append(keep, func(i issue) bool { return i.Assignee != nil && i.Assignee.ID == m.ID })
	}
	if in.Label != "" {
		ls, err := t.labels(ctx, in.Workspace)
		if err != nil {
			return nil, nil, err
		}
		l, err := find("label", in.Label, ls, func(l label) []string { return []string{l.Name} }, labelName)
		if err != nil {
			return nil, nil, err
		}
		keep = append(keep, func(i issue) bool { return slices.ContainsFunc(i.Labels, func(x label) bool { return x.ID == l.ID }) })
	}
	if in.Priority != "" {
		p, err := priority(in.Priority)
		if err != nil {
			return nil, nil, err
		}
		keep = append(keep, func(i issue) bool { return i.Priority != nil && *i.Priority == p })
	}
	var is []issue
	sort := url.Values{"sort": {cmp.Or(strings.ToLower(strings.TrimSpace(in.Sort)), "manual")}}
	if err := t.api.get(ctx, ws(in.Workspace, "projects", in.Project, "issues"), sort, &is); err != nil {
		return nil, nil, err
	}
	is = slices.DeleteFunc(is, func(i issue) bool {
		return slices.ContainsFunc(keep, func(k func(issue) bool) bool { return !k(i) })
	})
	return result(t.outs(in.Workspace, is))
}

func (t *tools) myIssues(ctx context.Context, _ *mcp.CallToolRequest, in wsIn) (*mcp.CallToolResult, any, error) {
	return t.userIssues(ctx, in.Workspace, "my-issues")
}

func (t *tools) watchingIssues(ctx context.Context, _ *mcp.CallToolRequest, in wsIn) (*mcp.CallToolResult, any, error) {
	return t.userIssues(ctx, in.Workspace, "watching")
}

func (t *tools) userIssues(ctx context.Context, slug, list string) (*mcp.CallToolResult, any, error) {
	var is []issue
	if err := t.api.get(ctx, ws(slug, list), nil, &is); err != nil {
		return nil, nil, err
	}
	return result(t.outs(slug, is))
}

func (t *tools) getIssue(ctx context.Context, _ *mcp.CallToolRequest, in issueIn) (*mcp.CallToolResult, any, error) {
	var i issue
	if err := t.api.get(ctx, ws(in.Workspace, "issues", in.Identifier), nil, &i); err != nil {
		return nil, nil, err
	}
	var tl struct {
		Comments []comment `json:"comments"`
		HasMore  bool      `json:"has_more"`
	}
	if err := t.api.get(ctx, ws(in.Workspace, "issues", in.Identifier, "timeline"), nil, &tl); err != nil {
		return nil, nil, err
	}
	o := t.out(in.Workspace, i)
	for _, c := range tl.Comments {
		o.Comments = append(o.Comments, commentOut{ID: c.ID, Author: name(c.Author), CreatedAt: c.CreatedAt, Text: plainText(c.Body)})
	}
	return result(o)
}

func (t *tools) createIssue(ctx context.Context, _ *mcp.CallToolRequest, in createIn) (*mcp.CallToolResult, any, error) {
	form, err := t.issueForm(ctx, in.Workspace, in.Project, fields{title: &in.Title, description: in.Description, status: in.Status,
		assignee: in.Assignee, priority: in.Priority, dueDate: in.DueDate, estimate: in.Estimate, labels: in.Labels, parent: in.Parent, cycle: in.Cycle})
	if err != nil {
		return nil, nil, err
	}
	if in.Description != nil && strings.TrimSpace(*in.Description) == "" {
		form.Del("description") // nothing to clear on a new issue
	}
	if in.After != "" || in.Before != "" {
		form.Set("after", strings.TrimSpace(in.After))
		form.Set("before", strings.TrimSpace(in.Before))
	}
	var i issue
	if err := t.api.post(ctx, ws(in.Workspace, "projects", in.Project, "issues"), form, &i); err != nil {
		return nil, nil, err
	}
	return result(t.out(in.Workspace, i))
}

func (t *tools) updateIssue(ctx context.Context, _ *mcp.CallToolRequest, in updateIn) (*mcp.CallToolResult, any, error) {
	key, _, _ := strings.Cut(in.Identifier, "-")
	form, err := t.issueForm(ctx, in.Workspace, key, fields{title: in.Title, description: in.Description, status: in.Status,
		assignee: in.Assignee, priority: in.Priority, dueDate: in.DueDate, estimate: in.Estimate, labels: in.Labels, parent: in.Parent, cycle: in.Cycle})
	if err != nil {
		return nil, nil, err
	}
	if len(form) == 0 {
		return nil, nil, errors.New("Give at least one field to change.")
	}
	var i issue
	if err := t.api.post(ctx, ws(in.Workspace, "issues", in.Identifier), form, &i); err != nil {
		return nil, nil, err
	}
	return result(t.out(in.Workspace, i))
}

func (t *tools) reorderIssues(ctx context.Context, _ *mcp.CallToolRequest, in reorderIssuesIn) (*mcp.CallToolResult, any, error) {
	// A bare number is an issue in the project.
	ident := func(s string) string {
		if s = strings.TrimSpace(s); s != "" && strings.Trim(s, "0123456789") == "" {
			return strings.TrimSpace(in.Project) + "-" + s
		}
		return s
	}
	type step struct{ identifier, after, before string }
	var steps []step
	switch {
	case len(in.Identifiers) >= 2:
		for k := 1; k < len(in.Identifiers); k++ {
			steps = append(steps, step{ident(in.Identifiers[k]), ident(in.Identifiers[k-1]), ""})
		}
	case in.Identifier != "" && (in.After != "" || in.Before != ""):
		steps = []step{{ident(in.Identifier), ident(in.After), ident(in.Before)}}
	default:
		return nil, nil, errors.New("Give identifiers (two or more, in order), or identifier with after or before.")
	}
	var moved []issue
	for _, s := range steps {
		var i issue
		if err := t.api.post(ctx, ws(in.Workspace, "issues", s.identifier, "move"), url.Values{"after": {s.after}, "before": {s.before}}, &i); err != nil {
			done := "None were moved."
			if len(moved) > 0 {
				ids := make([]string, len(moved))
				for k, m := range moved {
					ids[k] = m.Identifier
				}
				done = fmt.Sprintf("Moved %d: %s.", len(ids), strings.Join(ids, ", "))
			}
			return nil, nil, fmt.Errorf("Moving %s failed: %w\n%s", s.identifier, err, done)
		}
		moved = append(moved, i)
	}
	return result(t.outs(in.Workspace, moved))
}

func (t *tools) addComment(ctx context.Context, _ *mcp.CallToolRequest, in commentIn) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Body) == "" {
		return nil, nil, errors.New("Write a comment.")
	}
	var c comment
	if err := t.api.post(ctx, ws(in.Workspace, "issues", in.Identifier, "comments"), url.Values{"body": {toDoc(in.Body)}}, &c); err != nil {
		return nil, nil, err
	}
	return result(struct {
		commentOut
		URL string `json:"url"`
	}{commentOut{ID: c.ID, Author: name(c.Author), CreatedAt: c.CreatedAt, Text: plainText(c.Body)},
		t.app + "/" + url.PathEscape(in.Workspace) + "/issue/" + url.PathEscape(in.Identifier)})
}

// --- fields and names ---

// fields are an issue's settable fields as tools take them; nil leaves one unchanged.
type fields struct {
	title, description, status, assignee, priority, dueDate, parent, cycle *string
	estimate                                                               *int // 0 clears it
	labels                                                                 []string
}

// issueForm resolves names to ids and returns the API form for the fields given; project is the issue's project key,
// for its cycles.
func (t *tools) issueForm(ctx context.Context, slug, project string, f fields) (url.Values, error) {
	form := url.Values{}
	if f.title != nil {
		form.Set("title", strings.TrimSpace(*f.title))
	}
	if f.description != nil {
		form.Set("description", "")
		if strings.TrimSpace(*f.description) != "" {
			form.Set("description", toDoc(*f.description))
		}
	}
	if f.status != nil {
		ss, err := t.statuses(ctx, slug)
		if err != nil {
			return nil, err
		}
		s, err := find("status", *f.status, ss, func(s status) []string { return []string{s.Name} }, statusName)
		if err != nil {
			return nil, err
		}
		form.Set("status_id", s.ID)
	}
	if f.assignee != nil {
		form.Set("assignee_id", "")
		if strings.TrimSpace(*f.assignee) != "" {
			m, err := t.member(ctx, slug, *f.assignee)
			if err != nil {
				return nil, err
			}
			form.Set("assignee_id", m.ID)
		}
	}
	if f.priority != nil {
		p, err := priority(*f.priority)
		if err != nil {
			return nil, err
		}
		form.Set("priority", strconv.Itoa(p))
	}
	if f.dueDate != nil {
		form.Set("due_date", strings.TrimSpace(*f.dueDate))
	}
	if f.estimate != nil {
		form.Set("estimate", "")
		if *f.estimate != 0 {
			form.Set("estimate", strconv.Itoa(*f.estimate))
		}
	}
	if f.parent != nil {
		form.Set("parent", strings.TrimSpace(*f.parent))
	}
	if f.cycle != nil {
		form.Set("cycle", "")
		if strings.TrimSpace(*f.cycle) != "" {
			c, err := t.cycle(ctx, slug, project, *f.cycle)
			if err != nil {
				return nil, err
			}
			form.Set("cycle", c.ID)
		}
	}
	if f.labels != nil {
		form["label_ids"] = []string{""} // the field present and empty clears the labels
		if len(f.labels) > 0 {
			ls, err := t.labels(ctx, slug)
			if err != nil {
				return nil, err
			}
			form["label_ids"] = nil
			for _, n := range f.labels {
				l, err := find("label", n, ls, func(l label) []string { return []string{l.Name} }, labelName)
				if err != nil {
					return nil, err
				}
				form.Add("label_ids", l.ID)
			}
		}
	}
	return form, nil
}

func (t *tools) statuses(ctx context.Context, slug string) ([]status, error) {
	var ss []status
	return ss, t.api.get(ctx, ws(slug, "statuses"), nil, &ss)
}

func (t *tools) labels(ctx context.Context, slug string) ([]label, error) {
	var ls []label
	return ls, t.api.get(ctx, ws(slug, "labels"), nil, &ls)
}

func (t *tools) members(ctx context.Context, slug string) ([]member, error) {
	var res struct {
		Members []member `json:"members"`
	}
	return res.Members, t.api.get(ctx, ws(slug, "members"), nil, &res)
}

// member finds the workspace member with the email or name.
func (t *tools) member(ctx context.Context, slug, who string) (member, error) {
	ms, err := t.members(ctx, slug)
	if err != nil {
		return member{}, err
	}
	return find("member", who, ms, func(m member) []string { return []string{m.Email, m.Name} },
		func(m member) string { return m.Name + " <" + m.Email + ">" })
}

func statusName(s status) string { return s.Name }
func labelName(l label) string   { return l.Name }

// find returns the one item a key of which is want, ignoring case, or an error that lists the choices.
func find[T any](kind, want string, items []T, keys func(T) []string, show func(T) string) (T, error) {
	want = strings.TrimSpace(want)
	var hits []T
	for _, it := range items {
		if slices.ContainsFunc(keys(it), func(k string) bool { return strings.EqualFold(k, want) }) {
			hits = append(hits, it)
		}
	}
	if len(hits) == 1 {
		return hits[0], nil
	}
	choices := make([]string, len(items))
	for i, it := range items {
		choices[i] = show(it)
	}
	var zero T
	if len(hits) > 1 {
		return zero, fmt.Errorf("%q matches more than one %s. Choose one of: %s.", want, kind, strings.Join(choices, ", "))
	}
	if len(items) == 0 {
		return zero, fmt.Errorf("This workspace has no %ss.", kind)
	}
	return zero, fmt.Errorf("No %s %q. Choose one of: %s.", kind, want, strings.Join(choices, ", "))
}

// priority is the API's number for a priority name.
func priority(s string) (int, error) {
	if p := slices.Index(priorities, strings.ToLower(strings.TrimSpace(s))); p >= 0 {
		return p, nil
	}
	return 0, fmt.Errorf("No priority %q. Choose one of: %s.", s, strings.Join(priorities, ", "))
}

// idents are the refs' identifiers; nil for none.
func idents(rs []ref) []string {
	var ids []string
	for _, r := range rs {
		ids = append(ids, r.Identifier)
	}
	return ids
}

func name(p *person) string {
	if p == nil {
		return ""
	}
	return p.Name
}
