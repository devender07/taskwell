package server

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// projectTools registers the tools for projects and cycles.
func (t *tools) projectTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "create_project",
		Description: "Create a project. Its key (2–5 letters, such as ENG) prefixes its issue identifiers."}, t.createProject)
	mcp.AddTool(s, &mcp.Tool{Name: "update_project",
		Description: "Rename a project or change its description. Only the fields you give change."}, t.updateProject)
	mcp.AddTool(s, &mcp.Tool{Name: "archive_project",
		Description: "Archive a project: it leaves the project list and its issues become read-only."}, t.archiveProject)
	mcp.AddTool(s, &mcp.Tool{Name: "unarchive_project", Description: "Restore an archived project."}, t.unarchiveProject)
	mcp.AddTool(s, &mcp.Tool{Name: "list_cycles", Annotations: readOnly,
		Description: "List a project's cycles (sprints) with their numbers, dates, state and progress."}, t.listCycles)
	mcp.AddTool(s, &mcp.Tool{Name: "get_cycle", Annotations: readOnly,
		Description: "Read one cycle and its issues."}, t.getCycle)
	mcp.AddTool(s, &mcp.Tool{Name: "create_cycle",
		Description: "Create a cycle in a project. Cycles in a project can't overlap."}, t.createCycle)
	mcp.AddTool(s, &mcp.Tool{Name: "update_cycle",
		Description: "Rename a cycle or change its dates. Only the fields you give change; an empty name goes back to \"Cycle N\"."}, t.updateCycle)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_cycle", Annotations: destructive,
		Description: "Delete a cycle; its issues leave it."}, t.deleteCycle)
	mcp.AddTool(s, &mcp.Tool{Name: "rollover_cycle",
		Description: "Move a cycle's unfinished issues to another cycle (to, its number)."}, t.rolloverCycle)
}

type projectIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Project   string `json:"project" jsonschema:"project key, such as ENG"`
}

type createProjectIn struct {
	Workspace   string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Name        string `json:"name"`
	Key         string `json:"key" jsonschema:"2–5 letters, such as ENG"`
	Description string `json:"description,omitempty"`
}

type updateProjectIn struct {
	Workspace   string  `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Project     string  `json:"project" jsonschema:"project key, such as ENG"`
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty" jsonschema:"empty clears it"`
}

type cycleIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Project   string `json:"project" jsonschema:"project key, such as ENG"`
	Number    int    `json:"number" jsonschema:"cycle number, from list_cycles"`
}

type createCycleIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Project   string `json:"project" jsonschema:"project key, such as ENG"`
	StartsOn  string `json:"starts_on" jsonschema:"YYYY-MM-DD"`
	EndsOn    string `json:"ends_on" jsonschema:"YYYY-MM-DD"`
	Name      string `json:"name,omitempty" jsonschema:"\"Cycle N\" when left out"`
}

type updateCycleIn struct {
	Workspace string  `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Project   string  `json:"project" jsonschema:"project key, such as ENG"`
	Number    int     `json:"number" jsonschema:"cycle number, from list_cycles"`
	Name      *string `json:"name,omitempty"`
	StartsOn  *string `json:"starts_on,omitempty" jsonschema:"YYYY-MM-DD"`
	EndsOn    *string `json:"ends_on,omitempty" jsonschema:"YYYY-MM-DD"`
}

type rolloverIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Project   string `json:"project" jsonschema:"project key, such as ENG"`
	Number    int    `json:"number" jsonschema:"the cycle to move unfinished issues out of"`
	To        int    `json:"to" jsonschema:"the cycle number to move them to"`
}

type project struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Archived    bool   `json:"archived,omitempty"`
}

// cycleT is a cycle as the API answers; the tools drop its id.
type cycleT struct {
	ID       string `json:"id,omitzero"`
	Number   int    `json:"number"`
	Name     string `json:"name"`
	StartsOn string `json:"starts_on"`
	EndsOn   string `json:"ends_on"`
	State    string `json:"state"`
	Progress struct {
		Total     int `json:"total"`
		Started   int `json:"started"`
		Completed int `json:"completed"`
		Canceled  int `json:"canceled"`
	} `json:"progress"`
}

func (t *tools) createProject(ctx context.Context, _ *mcp.CallToolRequest, in createProjectIn) (*mcp.CallToolResult, any, error) {
	var p project
	form := url.Values{"name": {in.Name}, "key": {in.Key}, "description": {in.Description}}
	if err := t.api.post(ctx, ws(in.Workspace, "projects"), form, &p); err != nil {
		return nil, nil, err
	}
	return result(p)
}

func (t *tools) updateProject(ctx context.Context, _ *mcp.CallToolRequest, in updateProjectIn) (*mcp.CallToolResult, any, error) {
	// The API replaces name, key and description together, so the fields left out are read first.
	var p project
	if err := t.api.get(ctx, ws(in.Workspace, "projects", in.Project), nil, &p); err != nil {
		return nil, nil, err
	}
	if in.Name != nil {
		p.Name = *in.Name
	}
	if in.Description != nil {
		p.Description = *in.Description
	}
	form := url.Values{"name": {p.Name}, "key": {p.Key}, "description": {p.Description}}
	if err := t.api.post(ctx, ws(in.Workspace, "projects", p.Key), form, &p); err != nil {
		return nil, nil, err
	}
	return result(p)
}

func (t *tools) archiveProject(ctx context.Context, _ *mcp.CallToolRequest, in projectIn) (*mcp.CallToolResult, any, error) {
	var p project
	if err := t.api.post(ctx, ws(in.Workspace, "projects", in.Project, "archive"), url.Values{}, &p); err != nil {
		return nil, nil, err
	}
	return result(p)
}

func (t *tools) unarchiveProject(ctx context.Context, _ *mcp.CallToolRequest, in projectIn) (*mcp.CallToolResult, any, error) {
	var p project
	if err := t.api.post(ctx, ws(in.Workspace, "projects", in.Project, "unarchive"), url.Values{}, &p); err != nil {
		return nil, nil, err
	}
	return result(p)
}

func (t *tools) listCycles(ctx context.Context, _ *mcp.CallToolRequest, in projectIn) (*mcp.CallToolResult, any, error) {
	var cs []cycleT
	if err := t.api.get(ctx, ws(in.Workspace, "projects", in.Project, "cycles"), nil, &cs); err != nil {
		return nil, nil, err
	}
	for k := range cs {
		cs[k].ID = ""
	}
	return result(cs)
}

// cycleRes is GET …/cycles/{number}.
type cycleRes struct {
	Cycle  cycleT  `json:"cycle"`
	Issues []issue `json:"issues"`
}

// cycle returns the project's cycle with the number.
func (t *tools) cycle(ctx context.Context, slug, project, number string) (cycleT, error) {
	var res cycleRes
	err := t.api.get(ctx, ws(slug, "projects", project, "cycles", strings.TrimSpace(number)), nil, &res)
	return res.Cycle, err
}

func (t *tools) getCycle(ctx context.Context, _ *mcp.CallToolRequest, in cycleIn) (*mcp.CallToolResult, any, error) {
	var res cycleRes
	if err := t.api.get(ctx, cyclePath(in.Workspace, in.Project, in.Number), nil, &res); err != nil {
		return nil, nil, err
	}
	res.Cycle.ID = ""
	return result(struct {
		cycleT
		Issues []issueOut `json:"issues"`
	}{res.Cycle, t.outs(in.Workspace, res.Issues)})
}

func (t *tools) createCycle(ctx context.Context, _ *mcp.CallToolRequest, in createCycleIn) (*mcp.CallToolResult, any, error) {
	var c cycleT
	form := url.Values{"starts_on": {in.StartsOn}, "ends_on": {in.EndsOn}, "name": {in.Name}}
	if err := t.api.post(ctx, ws(in.Workspace, "projects", in.Project, "cycles"), form, &c); err != nil {
		return nil, nil, err
	}
	c.ID = ""
	return result(c)
}

func (t *tools) updateCycle(ctx context.Context, _ *mcp.CallToolRequest, in updateCycleIn) (*mcp.CallToolResult, any, error) {
	form := url.Values{}
	for k, v := range map[string]*string{"name": in.Name, "starts_on": in.StartsOn, "ends_on": in.EndsOn} {
		if v != nil {
			form.Set(k, *v)
		}
	}
	var c cycleT
	if err := t.api.post(ctx, cyclePath(in.Workspace, in.Project, in.Number), form, &c); err != nil {
		return nil, nil, err
	}
	c.ID = ""
	return result(c)
}

func (t *tools) deleteCycle(ctx context.Context, _ *mcp.CallToolRequest, in cycleIn) (*mcp.CallToolResult, any, error) {
	if err := t.api.del(ctx, cyclePath(in.Workspace, in.Project, in.Number), nil); err != nil {
		return nil, nil, err
	}
	return result(map[string]int{"deleted": in.Number})
}

func (t *tools) rolloverCycle(ctx context.Context, _ *mcp.CallToolRequest, in rolloverIn) (*mcp.CallToolResult, any, error) {
	var res struct {
		Moved int    `json:"moved"`
		Cycle cycleT `json:"cycle"`
	}
	path := cyclePath(in.Workspace, in.Project, in.Number) + "/rollover"
	if err := t.api.post(ctx, path, url.Values{"to": {strconv.Itoa(in.To)}}, &res); err != nil {
		return nil, nil, err
	}
	res.Cycle.ID = ""
	return result(res)
}

func cyclePath(slug, project string, number int) string {
	return ws(slug, "projects", project, "cycles", strconv.Itoa(number))
}
