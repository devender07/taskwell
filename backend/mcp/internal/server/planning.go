package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// planningTools registers the tools for project settings, members, milestones, updates, roadmap, stats and the trash.
func (t *tools) planningTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "get_project", Annotations: readOnly,
		Description: "Read one project: its fields, privacy, dates, health (from its latest update), members and milestones."}, t.getProject)
	mcp.AddTool(s, &mcp.Tool{Name: "list_project_members", Annotations: readOnly,
		Description: "List everyone who can see a project, with their role."}, t.listProjectMembers)
	mcp.AddTool(s, &mcp.Tool{Name: "add_project_members",
		Description: "Add workspace members to a private project, by email or name. A public project takes guests only."}, t.addProjectMembers)
	mcp.AddTool(s, &mcp.Tool{Name: "remove_project_member",
		Description: "Remove a member from a private project, by email or name."}, t.removeProjectMember)
	mcp.AddTool(s, &mcp.Tool{Name: "list_milestones", Annotations: readOnly,
		Description: "List a project's milestones with their ids and dates."}, t.listMilestones)
	mcp.AddTool(s, &mcp.Tool{Name: "create_milestone", Description: "Add a milestone to a project."}, t.createMilestone)
	mcp.AddTool(s, &mcp.Tool{Name: "update_milestone",
		Description: "Rename a milestone or move its date. Only the fields you give change."}, t.updateMilestone)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_milestone", Annotations: destructive, Description: "Delete a milestone."}, t.deleteMilestone)
	mcp.AddTool(s, &mcp.Tool{Name: "list_project_updates", Annotations: readOnly,
		Description: "List a project's updates, newest first, a page at a time (pass before from the last page)."}, t.listProjectUpdates)
	mcp.AddTool(s, &mcp.Tool{Name: "post_project_update",
		Description: "Post a project update: how it's going (on_track, at_risk or off_track) and a written note. " +
			"The latest update sets the project's health."}, t.postProjectUpdate)
	mcp.AddTool(s, &mcp.Tool{Name: "edit_project_update",
		Description: "Change a project update's health or text. Only the fields you give change."}, t.editProjectUpdate)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_project_update", Annotations: destructive,
		Description: "Delete a project update."}, t.deleteProjectUpdate)
	mcp.AddTool(s, &mcp.Tool{Name: "get_roadmap", Annotations: readOnly,
		Description: "Read the workspace roadmap: each active project's dates, health, progress, milestones and cycles."}, t.getRoadmap)
	mcp.AddTool(s, &mcp.Tool{Name: "project_stats", Annotations: readOnly,
		Description: "Count each active project's open, done, canceled and overdue issues."}, t.projectStats)
	mcp.AddTool(s, &mcp.Tool{Name: "member_analytics", Annotations: readOnly,
		Description: "Read a member's workload and the last 12 weeks of their work: open and overdue issues, completed, " +
			"created and comments per week, cycle time and current cycles. Yours by default; anyone else's needs analytics permission."},
		t.memberAnalytics)
	mcp.AddTool(s, &mcp.Tool{Name: "team_analytics", Annotations: readOnly,
		Description: "Read each member's open, overdue and recently completed issues and cycle time. Needs analytics permission."}, t.teamAnalytics)
	mcp.AddTool(s, &mcp.Tool{Name: "list_trash", Annotations: readOnly,
		Description: "List the workspace's deleted issues, newest first, a page at a time (pass before from the last page)."}, t.listTrash)
	mcp.AddTool(s, &mcp.Tool{Name: "restore_issue", Description: "Take a deleted issue out of the trash."}, t.restoreIssue)
	mcp.AddTool(s, &mcp.Tool{Name: "purge_issue", Annotations: destructive,
		Description: "Delete an issue in the trash for good. This can't be undone."}, t.purgeIssue)
	mcp.AddTool(s, &mcp.Tool{Name: "empty_trash", Annotations: destructive,
		Description: "Delete every issue in the trash you can see for good. This can't be undone."}, t.emptyTrash)
}

// --- inputs ---

type projectMembersIn struct {
	Workspace string   `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Project   string   `json:"project" jsonschema:"project key, such as ENG"`
	Members   []string `json:"members" jsonschema:"members' emails or names"`
}

type projectMemberIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Project   string `json:"project" jsonschema:"project key, such as ENG"`
	Member    string `json:"member" jsonschema:"member's email or name"`
}

type createMilestoneIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Project   string `json:"project" jsonschema:"project key, such as ENG"`
	Name      string `json:"name"`
	Date      string `json:"date" jsonschema:"YYYY-MM-DD"`
}

type milestoneIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Project   string `json:"project" jsonschema:"project key, such as ENG"`
	Milestone string `json:"milestone" jsonschema:"milestone id or name, from list_milestones"`
}

type updateMilestoneIn struct {
	Workspace string  `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Project   string  `json:"project" jsonschema:"project key, such as ENG"`
	Milestone string  `json:"milestone" jsonschema:"milestone id or name, from list_milestones"`
	Name      *string `json:"name,omitempty"`
	Date      *string `json:"date,omitempty" jsonschema:"YYYY-MM-DD"`
}

type projectPageIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Project   string `json:"project" jsonschema:"project key, such as ENG"`
	Before    string `json:"before,omitempty" jsonschema:"the before value of the previous page"`
}

type postUpdateIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Project   string `json:"project" jsonschema:"project key, such as ENG"`
	Health    string `json:"health" jsonschema:"on_track, at_risk or off_track"`
	Body      string `json:"body" jsonschema:"plain text; blank lines separate paragraphs"`
}

type editUpdateIn struct {
	Workspace string  `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	ID        string  `json:"id" jsonschema:"update id, from list_project_updates"`
	Health    *string `json:"health,omitempty" jsonschema:"on_track, at_risk or off_track"`
	Body      *string `json:"body,omitempty" jsonschema:"plain text; blank lines separate paragraphs"`
}

type projectUpdateIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	ID        string `json:"id" jsonschema:"update id, from list_project_updates"`
}

type analyticsIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Member    string `json:"member,omitempty" jsonschema:"member's email or name; you when left out"`
}

// --- API shapes ---

type milestone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Date string `json:"date"`
}

type projectUpdate struct {
	ID        string          `json:"id"`
	Health    string          `json:"health"`
	Body      json.RawMessage `json:"body"`
	Author    *person         `json:"author"`
	CreatedAt string          `json:"created_at"`
	EditedAt  *string         `json:"edited_at"`
	CanEdit   bool            `json:"can_edit"`
}

type updateOut struct {
	ID        string  `json:"id"`
	Health    string  `json:"health"`
	Author    string  `json:"author,omitempty"`
	CreatedAt string  `json:"created_at"`
	EditedAt  *string `json:"edited_at,omitempty"`
	CanEdit   bool    `json:"can_edit"`
	Text      string  `json:"text"`
}

func outUpdate(u projectUpdate) updateOut {
	return updateOut{ID: u.ID, Health: u.Health, Author: name(u.Author), CreatedAt: u.CreatedAt, EditedAt: u.EditedAt,
		CanEdit: u.CanEdit, Text: plainText(u.Body)}
}

// --- projects and members ---

func (t *tools) getProject(ctx context.Context, _ *mcp.CallToolRequest, in projectIn) (*mcp.CallToolResult, any, error) {
	var p project
	if err := t.api.get(ctx, ws(in.Workspace, "projects", in.Project), nil, &p); err != nil {
		return nil, nil, err
	}
	var ms []milestone
	if err := t.api.get(ctx, ws(in.Workspace, "projects", p.Key, "milestones"), nil, &ms); err != nil {
		return nil, nil, err
	}
	return result(struct {
		project
		Milestones []milestone `json:"milestones"`
		URL        string      `json:"url"`
	}{p, ms, t.app + "/" + url.PathEscape(in.Workspace) + "/projects/" + url.PathEscape(p.Key)})
}

func (t *tools) listProjectMembers(ctx context.Context, _ *mcp.CallToolRequest, in projectIn) (*mcp.CallToolResult, any, error) {
	var ms []struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		Role  struct {
			Name string `json:"name"`
		} `json:"role"`
		Guest bool `json:"guest"`
	}
	if err := t.api.get(ctx, ws(in.Workspace, "projects", in.Project, "people"), nil, &ms); err != nil {
		return nil, nil, err
	}
	type out struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		Role  string `json:"role"`
		Guest bool   `json:"guest,omitempty"`
	}
	o := make([]out, len(ms))
	for k, m := range ms {
		o[k] = out{m.Name, m.Email, m.Role.Name, m.Guest}
	}
	return result(o)
}

func (t *tools) addProjectMembers(ctx context.Context, _ *mcp.CallToolRequest, in projectMembersIn) (*mcp.CallToolResult, any, error) {
	if len(in.Members) == 0 {
		return nil, nil, errors.New("Name at least one member.")
	}
	ids, err := t.memberIDs(ctx, in.Workspace, in.Members)
	if err != nil {
		return nil, nil, err
	}
	var p project
	if err := t.api.post(ctx, ws(in.Workspace, "projects", in.Project, "members"), url.Values{"user_ids": ids}, &p); err != nil {
		return nil, nil, err
	}
	return result(p)
}

func (t *tools) removeProjectMember(ctx context.Context, _ *mcp.CallToolRequest, in projectMemberIn) (*mcp.CallToolResult, any, error) {
	m, err := t.member(ctx, in.Workspace, in.Member)
	if err != nil {
		return nil, nil, err
	}
	var p project
	if err := t.api.del(ctx, ws(in.Workspace, "projects", in.Project, "members", m.ID), &p); err != nil {
		return nil, nil, err
	}
	return result(p)
}

// memberIDs resolves the workspace members' emails or names to their ids.
func (t *tools) memberIDs(ctx context.Context, slug string, who []string) ([]string, error) {
	ms, err := t.members(ctx, slug)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(who))
	for k, w := range who {
		m, err := find("member", w, ms, func(m member) []string { return []string{m.Email, m.Name} },
			func(m member) string { return m.Name + " <" + m.Email + ">" })
		if err != nil {
			return nil, err
		}
		ids[k] = m.ID
	}
	return ids, nil
}

// --- milestones ---

func (t *tools) milestones(ctx context.Context, slug, project string) ([]milestone, error) {
	var ms []milestone
	return ms, t.api.get(ctx, ws(slug, "projects", project, "milestones"), nil, &ms)
}

// milestone finds the project's milestone with the id or name.
func (t *tools) milestone(ctx context.Context, slug, project, want string) (milestone, error) {
	ms, err := t.milestones(ctx, slug, project)
	if err != nil {
		return milestone{}, err
	}
	return find("milestone", want, ms, func(m milestone) []string { return []string{m.ID, m.Name} },
		func(m milestone) string { return m.Name + " (" + m.Date + ")" })
}

func (t *tools) listMilestones(ctx context.Context, _ *mcp.CallToolRequest, in projectIn) (*mcp.CallToolResult, any, error) {
	ms, err := t.milestones(ctx, in.Workspace, in.Project)
	if err != nil {
		return nil, nil, err
	}
	return result(ms)
}

func (t *tools) createMilestone(ctx context.Context, _ *mcp.CallToolRequest, in createMilestoneIn) (*mcp.CallToolResult, any, error) {
	var m milestone
	form := url.Values{"name": {in.Name}, "date": {in.Date}}
	if err := t.api.post(ctx, ws(in.Workspace, "projects", in.Project, "milestones"), form, &m); err != nil {
		return nil, nil, err
	}
	return result(m)
}

func (t *tools) updateMilestone(ctx context.Context, _ *mcp.CallToolRequest, in updateMilestoneIn) (*mcp.CallToolResult, any, error) {
	// The API replaces name and date together, so the one left out is read first.
	m, err := t.milestone(ctx, in.Workspace, in.Project, in.Milestone)
	if err != nil {
		return nil, nil, err
	}
	if in.Name != nil {
		m.Name = *in.Name
	}
	if in.Date != nil {
		m.Date = *in.Date
	}
	form := url.Values{"name": {m.Name}, "date": {m.Date}}
	if err := t.api.post(ctx, ws(in.Workspace, "projects", in.Project, "milestones", m.ID), form, &m); err != nil {
		return nil, nil, err
	}
	return result(m)
}

func (t *tools) deleteMilestone(ctx context.Context, _ *mcp.CallToolRequest, in milestoneIn) (*mcp.CallToolResult, any, error) {
	m, err := t.milestone(ctx, in.Workspace, in.Project, in.Milestone)
	if err != nil {
		return nil, nil, err
	}
	if err := t.api.del(ctx, ws(in.Workspace, "projects", in.Project, "milestones", m.ID), nil); err != nil {
		return nil, nil, err
	}
	return result(map[string]string{"deleted": m.Name})
}

// --- project updates ---

func (t *tools) listProjectUpdates(ctx context.Context, _ *mcp.CallToolRequest, in projectPageIn) (*mcp.CallToolResult, any, error) {
	var res struct {
		Updates []projectUpdate `json:"updates"`
		HasMore bool            `json:"has_more"`
	}
	if err := t.api.get(ctx, ws(in.Workspace, "projects", in.Project, "updates"), before(in.Before), &res); err != nil {
		return nil, nil, err
	}
	o := struct {
		Updates []updateOut `json:"updates"`
		Before  string      `json:"before,omitempty"`
	}{Updates: make([]updateOut, len(res.Updates))}
	for k, u := range res.Updates {
		o.Updates[k] = outUpdate(u)
	}
	if res.HasMore && len(res.Updates) > 0 {
		o.Before = res.Updates[len(res.Updates)-1].CreatedAt
	}
	return result(o)
}

func (t *tools) postProjectUpdate(ctx context.Context, _ *mcp.CallToolRequest, in postUpdateIn) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Body) == "" {
		return nil, nil, errors.New("Write an update.")
	}
	var u projectUpdate
	form := url.Values{"health": {strings.TrimSpace(in.Health)}, "body": {toDoc(in.Body)}}
	if err := t.api.post(ctx, ws(in.Workspace, "projects", in.Project, "updates"), form, &u); err != nil {
		return nil, nil, err
	}
	return result(outUpdate(u))
}

func (t *tools) editProjectUpdate(ctx context.Context, _ *mcp.CallToolRequest, in editUpdateIn) (*mcp.CallToolResult, any, error) {
	form := url.Values{}
	if in.Health != nil {
		form.Set("health", strings.TrimSpace(*in.Health))
	}
	if in.Body != nil {
		if strings.TrimSpace(*in.Body) == "" {
			return nil, nil, errors.New("Write an update.")
		}
		form.Set("body", toDoc(*in.Body))
	}
	if len(form) == 0 {
		return nil, nil, errors.New("Give health or body to change.")
	}
	var u projectUpdate
	if err := t.api.post(ctx, ws(in.Workspace, "project-updates", in.ID), form, &u); err != nil {
		return nil, nil, err
	}
	return result(outUpdate(u))
}

func (t *tools) deleteProjectUpdate(ctx context.Context, _ *mcp.CallToolRequest, in projectUpdateIn) (*mcp.CallToolResult, any, error) {
	if err := t.api.del(ctx, ws(in.Workspace, "project-updates", in.ID), nil); err != nil {
		return nil, nil, err
	}
	return result(map[string]string{"deleted": in.ID})
}

// --- roadmap, stats and analytics: the API's answers as they are ---

func (t *tools) raw(ctx context.Context, path string) (*mcp.CallToolResult, any, error) {
	var v json.RawMessage
	if err := t.api.get(ctx, path, nil, &v); err != nil {
		return nil, nil, err
	}
	return result(v)
}

func (t *tools) getRoadmap(ctx context.Context, _ *mcp.CallToolRequest, in wsIn) (*mcp.CallToolResult, any, error) {
	return t.raw(ctx, ws(in.Workspace, "roadmap"))
}

func (t *tools) projectStats(ctx context.Context, _ *mcp.CallToolRequest, in wsIn) (*mcp.CallToolResult, any, error) {
	return t.raw(ctx, ws(in.Workspace, "project-stats"))
}

func (t *tools) teamAnalytics(ctx context.Context, _ *mcp.CallToolRequest, in wsIn) (*mcp.CallToolResult, any, error) {
	return t.raw(ctx, ws(in.Workspace, "analytics", "team"))
}

func (t *tools) memberAnalytics(ctx context.Context, _ *mcp.CallToolRequest, in analyticsIn) (*mcp.CallToolResult, any, error) {
	var m member
	if strings.TrimSpace(in.Member) == "" {
		if err := t.api.get(ctx, "/api/me", nil, &m); err != nil {
			return nil, nil, err
		}
	} else {
		var err error
		if m, err = t.member(ctx, in.Workspace, in.Member); err != nil {
			return nil, nil, err
		}
	}
	return t.raw(ctx, ws(in.Workspace, "members", m.ID, "analytics"))
}

// --- trash ---

func (t *tools) listTrash(ctx context.Context, _ *mcp.CallToolRequest, in pageIn) (*mcp.CallToolResult, any, error) {
	var res struct {
		Issues []struct {
			Identifier string `json:"identifier"`
			Title      string `json:"title"`
			Project    struct {
				Key string `json:"key"`
			} `json:"project"`
			DeletedAt  string  `json:"deleted_at"`
			DeletedBy  *person `json:"deleted_by"`
			CanRestore bool    `json:"can_restore"`
		} `json:"issues"`
		HasMore bool `json:"has_more"`
	}
	if err := t.api.get(ctx, ws(in.Workspace, "trash"), before(in.Before), &res); err != nil {
		return nil, nil, err
	}
	type out struct {
		Identifier string `json:"identifier"`
		Title      string `json:"title"`
		Project    string `json:"project"`
		DeletedAt  string `json:"deleted_at"`
		DeletedBy  string `json:"deleted_by,omitempty"`
		CanRestore bool   `json:"can_restore"`
	}
	o := struct {
		Issues []out  `json:"issues"`
		Before string `json:"before,omitempty"`
	}{Issues: make([]out, len(res.Issues))}
	for k, i := range res.Issues {
		o.Issues[k] = out{i.Identifier, i.Title, i.Project.Key, i.DeletedAt, name(i.DeletedBy), i.CanRestore}
	}
	if res.HasMore && len(res.Issues) > 0 {
		o.Before = res.Issues[len(res.Issues)-1].DeletedAt
	}
	return result(o)
}

func (t *tools) restoreIssue(ctx context.Context, _ *mcp.CallToolRequest, in issueIn) (*mcp.CallToolResult, any, error) {
	var i issue
	if err := t.api.post(ctx, ws(in.Workspace, "trash", in.Identifier, "restore"), url.Values{}, &i); err != nil {
		return nil, nil, err
	}
	return result(t.out(in.Workspace, i))
}

func (t *tools) purgeIssue(ctx context.Context, _ *mcp.CallToolRequest, in issueIn) (*mcp.CallToolResult, any, error) {
	if err := t.api.del(ctx, ws(in.Workspace, "trash", in.Identifier), nil); err != nil {
		return nil, nil, err
	}
	return result(map[string]string{"purged": in.Identifier})
}

func (t *tools) emptyTrash(ctx context.Context, _ *mcp.CallToolRequest, in wsIn) (*mcp.CallToolResult, any, error) {
	if err := t.api.del(ctx, ws(in.Workspace, "trash"), nil); err != nil {
		return nil, nil, err
	}
	return result(map[string]bool{"emptied": true})
}
