package server

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var destructive = &mcp.ToolAnnotations{DestructiveHint: new(true)}

// issueTools registers the tools for issue links, watching, comments, activity, labels and statuses.
func (t *tools) issueTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "delete_issue", Annotations: destructive,
		Description: "Delete an issue."}, t.deleteIssue)
	mcp.AddTool(s, &mcp.Tool{Name: "link_issues",
		Description: "Link two issues: type blocks (the issue blocks other) or blocked_by (the issue is blocked by other)."}, t.linkIssues)
	mcp.AddTool(s, &mcp.Tool{Name: "unlink_issues", Annotations: destructive,
		Description: "Remove the blocking link between two issues, whichever way it goes."}, t.unlinkIssues)
	mcp.AddTool(s, &mcp.Tool{Name: "watch_issue",
		Description: "Start (watch true) or stop (watch false) watching an issue for notifications."}, t.watchIssue)
	mcp.AddTool(s, &mcp.Tool{Name: "update_comment",
		Description: "Replace a comment's text. Comment ids come from get_issue or issue_activity."}, t.updateComment)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_comment", Annotations: destructive,
		Description: "Delete a comment. Comment ids come from get_issue or issue_activity."}, t.deleteComment)
	mcp.AddTool(s, &mcp.Tool{Name: "issue_activity", Annotations: readOnly,
		Description: "An issue's comments and change events, newest first. When has_more, pass before to get older ones."}, t.issueActivity)
	mcp.AddTool(s, &mcp.Tool{Name: "workspace_activity", Annotations: readOnly,
		Description: "Recent changes and comments across a workspace's active projects, newest first. When has_more, pass before to get older ones."},
		t.workspaceActivity)
	mcp.AddTool(s, &mcp.Tool{Name: "create_label", Description: "Create an issue label."}, t.createLabel)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_label", Annotations: destructive,
		Description: "Delete a label; issues lose it."}, t.deleteLabel)
	mcp.AddTool(s, &mcp.Tool{Name: "create_status",
		Description: "Create an issue status in a category: backlog, unstarted, started, completed or canceled."}, t.createStatus)
}

type linkIn struct {
	Workspace  string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Identifier string `json:"identifier" jsonschema:"issue identifier, such as ENG-142"`
	Type       string `json:"type" jsonschema:"blocks or blocked_by"`
	Other      string `json:"other" jsonschema:"the other issue's identifier"`
}

type unlinkIn struct {
	Workspace  string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Identifier string `json:"identifier" jsonschema:"issue identifier, such as ENG-142"`
	Other      string `json:"other" jsonschema:"the linked issue's identifier"`
}

type watchIn struct {
	Workspace  string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Identifier string `json:"identifier" jsonschema:"issue identifier, such as ENG-142"`
	Watch      bool   `json:"watch" jsonschema:"true to watch, false to stop"`
}

type commentIDIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	CommentID string `json:"comment_id"`
}

type editCommentIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	CommentID string `json:"comment_id"`
	Body      string `json:"body" jsonschema:"plain text; blank lines separate paragraphs"`
}

type issueActivityIn struct {
	Workspace  string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Identifier string `json:"identifier" jsonschema:"issue identifier, such as ENG-142"`
	Before     string `json:"before,omitempty" jsonschema:"the before value of the previous page"`
}

type wsActivityIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Before    string `json:"before,omitempty" jsonschema:"the before value of the previous page"`
}

type labelIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Name      string `json:"name"`
	Color     string `json:"color,omitempty" jsonschema:"hex color such as #5e6ad2; the default when left out"`
}

type nameIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Name      string `json:"name"`
}

type statusIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Name      string `json:"name"`
	Category  string `json:"category" jsonschema:"backlog, unstarted, started, completed or canceled"`
}

// activityOut is a comment or an event on an issue's timeline or in a workspace's activity.
type activityOut struct {
	Type      string          `json:"type"` // comment or event
	ID        string          `json:"id,omitempty"`
	Issue     string          `json:"issue,omitempty"`
	Actor     string          `json:"actor,omitempty"`
	Kind      string          `json:"kind,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	Text      string          `json:"text,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

type activityPage struct {
	Items   []activityOut `json:"items"`
	HasMore bool          `json:"has_more"`
	Before  string        `json:"before,omitempty"` // pass to get the next page
}

func page(items []activityOut, more bool) activityPage {
	p := activityPage{Items: items, HasMore: more}
	if more && len(items) > 0 {
		p.Before = items[len(items)-1].CreatedAt.Format(time.RFC3339Nano)
	}
	return p
}

func (t *tools) issueURL(slug, identifier string) string {
	return t.app + "/" + url.PathEscape(slug) + "/issue/" + url.PathEscape(identifier)
}

func (t *tools) deleteIssue(ctx context.Context, _ *mcp.CallToolRequest, in issueIn) (*mcp.CallToolResult, any, error) {
	if err := t.api.del(ctx, ws(in.Workspace, "issues", in.Identifier), nil); err != nil {
		return nil, nil, err
	}
	return result(map[string]string{"deleted": in.Identifier})
}

func (t *tools) linkIssues(ctx context.Context, _ *mcp.CallToolRequest, in linkIn) (*mcp.CallToolResult, any, error) {
	var i issue
	form := url.Values{"type": {strings.ToLower(strings.TrimSpace(in.Type))}, "issue": {strings.TrimSpace(in.Other)}}
	if err := t.api.post(ctx, ws(in.Workspace, "issues", in.Identifier, "links"), form, &i); err != nil {
		return nil, nil, err
	}
	return result(t.out(in.Workspace, i))
}

func (t *tools) unlinkIssues(ctx context.Context, _ *mcp.CallToolRequest, in unlinkIn) (*mcp.CallToolResult, any, error) {
	var i issue
	if err := t.api.del(ctx, ws(in.Workspace, "issues", in.Identifier, "links", strings.TrimSpace(in.Other)), &i); err != nil {
		return nil, nil, err
	}
	return result(t.out(in.Workspace, i))
}

func (t *tools) watchIssue(ctx context.Context, _ *mcp.CallToolRequest, in watchIn) (*mcp.CallToolResult, any, error) {
	var res struct {
		Watching bool `json:"watching"`
	}
	path := ws(in.Workspace, "issues", in.Identifier, "watch")
	var err error
	if in.Watch {
		err = t.api.post(ctx, path, url.Values{}, &res)
	} else {
		err = t.api.del(ctx, path, &res)
	}
	if err != nil {
		return nil, nil, err
	}
	return result(res)
}

func (t *tools) updateComment(ctx context.Context, _ *mcp.CallToolRequest, in editCommentIn) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Body) == "" {
		return nil, nil, errors.New("Write a comment.")
	}
	var c comment
	if err := t.api.post(ctx, ws(in.Workspace, "comments", in.CommentID), url.Values{"body": {toDoc(in.Body)}}, &c); err != nil {
		return nil, nil, err
	}
	return result(commentOut{ID: c.ID, Author: name(c.Author), CreatedAt: c.CreatedAt, Text: plainText(c.Body)})
}

func (t *tools) deleteComment(ctx context.Context, _ *mcp.CallToolRequest, in commentIDIn) (*mcp.CallToolResult, any, error) {
	if err := t.api.del(ctx, ws(in.Workspace, "comments", in.CommentID), nil); err != nil {
		return nil, nil, err
	}
	return result(map[string]string{"deleted": in.CommentID})
}

// timed is a timeline comment or event as the API answers.
type timed struct {
	ID        string          `json:"id"`
	Author    *person         `json:"author"`
	Actor     *person         `json:"actor"`
	Body      json.RawMessage `json:"body"`
	Kind      string          `json:"kind"`
	Data      json.RawMessage `json:"data"`
	CreatedAt time.Time       `json:"created_at"`
}

func (t *tools) issueActivity(ctx context.Context, _ *mcp.CallToolRequest, in issueActivityIn) (*mcp.CallToolResult, any, error) {
	var tl struct {
		Comments []timed `json:"comments"`
		Events   []timed `json:"events"`
		HasMore  bool    `json:"has_more"`
	}
	if err := t.api.get(ctx, ws(in.Workspace, "issues", in.Identifier, "timeline"), before(in.Before), &tl); err != nil {
		return nil, nil, err
	}
	items := make([]activityOut, 0, len(tl.Comments)+len(tl.Events))
	for _, c := range tl.Comments {
		items = append(items, activityOut{Type: "comment", ID: c.ID, Actor: name(c.Author), Text: plainText(c.Body), CreatedAt: c.CreatedAt})
	}
	for _, e := range tl.Events {
		items = append(items, activityOut{Type: "event", Actor: name(e.Actor), Kind: e.Kind, Data: e.Data, CreatedAt: e.CreatedAt})
	}
	slices.SortStableFunc(items, func(a, b activityOut) int { return b.CreatedAt.Compare(a.CreatedAt) })
	return result(page(items, tl.HasMore))
}

func (t *tools) workspaceActivity(ctx context.Context, _ *mcp.CallToolRequest, in wsActivityIn) (*mcp.CallToolResult, any, error) {
	var res struct {
		Items []struct {
			timed
			Type    string  `json:"type"`
			Issue   ref     `json:"issue"`
			Excerpt *string `json:"excerpt"`
		} `json:"items"`
		HasMore bool `json:"has_more"`
	}
	if err := t.api.get(ctx, ws(in.Workspace, "activity"), before(in.Before), &res); err != nil {
		return nil, nil, err
	}
	items := make([]activityOut, len(res.Items))
	for k, a := range res.Items {
		items[k] = activityOut{Type: a.Type, Issue: a.Issue.Identifier, Actor: name(a.Actor), Kind: a.Kind, Data: a.Data, CreatedAt: a.CreatedAt}
		if a.Excerpt != nil {
			items[k].ID, items[k].Text = a.ID, *a.Excerpt
		}
	}
	return result(page(items, res.HasMore))
}

// before is the query for a page before the cursor; none for the first page.
func before(cursor string) url.Values {
	if cursor = strings.TrimSpace(cursor); cursor != "" {
		return url.Values{"before": {cursor}}
	}
	return nil
}

func (t *tools) createLabel(ctx context.Context, _ *mcp.CallToolRequest, in labelIn) (*mcp.CallToolResult, any, error) {
	var l struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	form := url.Values{"name": {in.Name}, "color": {cmp.Or(strings.TrimSpace(in.Color), "#5e6ad2")}}
	if err := t.api.post(ctx, ws(in.Workspace, "labels"), form, &l); err != nil {
		return nil, nil, err
	}
	return result(l)
}

func (t *tools) deleteLabel(ctx context.Context, _ *mcp.CallToolRequest, in nameIn) (*mcp.CallToolResult, any, error) {
	ls, err := t.labels(ctx, in.Workspace)
	if err != nil {
		return nil, nil, err
	}
	l, err := find("label", in.Name, ls, func(l label) []string { return []string{l.Name} }, labelName)
	if err != nil {
		return nil, nil, err
	}
	if err := t.api.del(ctx, ws(in.Workspace, "labels", l.ID), nil); err != nil {
		return nil, nil, err
	}
	return result(map[string]string{"deleted": l.Name})
}

func (t *tools) createStatus(ctx context.Context, _ *mcp.CallToolRequest, in statusIn) (*mcp.CallToolResult, any, error) {
	var s status
	form := url.Values{"name": {in.Name}, "category": {strings.ToLower(strings.TrimSpace(in.Category))}}
	if err := t.api.post(ctx, ws(in.Workspace, "statuses"), form, &s); err != nil {
		return nil, nil, err
	}
	return result(struct {
		Name     string `json:"name"`
		Category string `json:"category"`
	}{s.Name, s.Category})
}
