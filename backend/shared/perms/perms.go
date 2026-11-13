// Package perms is the workspace permission catalog, shared by the user API (which enforces it) and the admin API
// (which edits the roles new workspaces start with).
package perms

import "slices"

// Permission is one entry of the catalog; Active is false until the feature it gates ships.
type Permission struct {
	Key    string `json:"key"`
	Group  string `json:"group"`
	Label  string `json:"label"`
	Active bool   `json:"active"`
}

const (
	// WorkspaceEdit gates renaming the workspace; it also lets a member edit and delete others' shared views.
	WorkspaceEdit   = "workspace.edit"
	WorkspaceDelete = "workspace.delete"
	// WorkspacesCreate lets a member create new workspaces; checked in SQL by can_create_workspace.
	WorkspacesCreate = "workspaces.create"
	MembersInvite    = "members.invite"
	MembersManage    = "members.manage"
	RolesManage      = "roles.manage"

	// ProjectsManage gates creating, editing and archiving projects.
	ProjectsManage = "projects.manage"
	// IssuesCreate gates creating issues.
	IssuesCreate = "issues.create"
	// IssuesEditAny lets a member edit and delete issues they neither created nor are assigned.
	IssuesEditAny = "issues.edit_any"
	// IssuesStatusAny lets a member change the status of any issue they can see (QA reopening work), nothing else.
	IssuesStatusAny = "issues.status_any"
	// WorkflowManage gates creating, editing, reordering and deleting statuses and labels.
	WorkflowManage = "workflow.manage"
	// CommentsCreate gates commenting on issues and editing one's own comments.
	CommentsCreate = "comments.create"
	// CommentsManage lets a member edit and delete anyone's comments.
	CommentsManage = "comments.manage"
	// CyclesManage gates creating, editing, deleting and rolling over cycles.
	CyclesManage = "cycles.manage"
	// AITestsRun gates starting and cancelling AI test runs.
	AITestsRun = "ai_tests.run"
	// TestsManage gates editing an issue's test cases and running them by hand.
	TestsManage = "tests.manage"
	// AITestsManage gates the workspace's test sites and test accounts.
	AITestsManage = "ai_tests.manage"
	// IntegrationsManage gates connecting GitHub and GitLab and managing outbound webhooks.
	IntegrationsManage = "integrations.manage"
	// FeedbackManage gates the public feedback form and archiving and deleting feedback.
	FeedbackManage = "feedback.manage"
	// AnalyticsView lets a member see other members' analytics and the team page.
	AnalyticsView = "analytics.view"
	// WorkspaceExport lets a member download the workspace's data (docs/workspace-export-plan.md).
	WorkspaceExport = "workspace.export"
	// AuditView lets a member read the workspace audit log (docs/workspace-audit-plan.md).
	AuditView = "audit.view"
)

// Catalog is every permission in display order. Don't modify it.
var Catalog = []Permission{
	{WorkspaceEdit, "Workspace", "Edit workspace name and URL", true},
	{WorkspaceDelete, "Workspace", "Delete the workspace", true},
	{WorkspacesCreate, "Workspace", "Create new workspaces", true},
	{IntegrationsManage, "Workspace", "Manage GitHub, GitLab and webhooks", true},
	{WorkspaceExport, "Workspace", "Export workspace data", true},
	{AuditView, "Workspace", "See the audit log", true},
	{MembersInvite, "Members", "Invite people, resend and cancel invites", true},
	{MembersManage, "Members", "Change members' roles and remove members", true},
	{RolesManage, "Members", "Create, edit and delete roles", true},
	{ProjectsManage, "Projects", "Create, edit and archive projects", true},
	{IssuesCreate, "Issues", "Create issues", true},
	{IssuesEditAny, "Issues", "Edit and delete anyone's issues", true},
	{IssuesStatusAny, "Issues", "Change the status of anyone's issues", true},
	{WorkflowManage, "Issues", "Manage statuses and labels", true},
	{CyclesManage, "Cycles", "Create, edit and delete cycles", true},
	{CommentsCreate, "Comments", "Comment on issues", true},
	{CommentsManage, "Comments", "Edit and delete anyone's comments", true},
	{AITestsRun, "Testing", "Run AI tests on issues", true},
	{TestsManage, "Testing", "Edit test cases and run tests manually", true},
	{AITestsManage, "Testing", "Manage test sites and test accounts", true},
	{FeedbackManage, "Feedback", "Manage the feedback form, archive and delete feedback", true},
	{AnalyticsView, "Analytics", "See other members' analytics and the team page", true},
}

// All is every key in catalog order.
func All() []string {
	keys := make([]string, len(Catalog))
	for i, p := range Catalog {
		keys[i] = p.Key
	}
	return keys
}

// Active is the catalog's active entries.
func Active() []Permission {
	return slices.DeleteFunc(slices.Clone(Catalog), func(p Permission) bool { return !p.Active })
}

// GuestDenied reports whether a Guest role may not hold the permission: none of the Workspace, Members and
// Analytics groups.
func GuestDenied(key string) bool {
	i := slices.IndexFunc(Catalog, func(p Permission) bool { return p.Key == key })
	return i >= 0 && slices.Contains([]string{"Workspace", "Members", "Analytics"}, Catalog[i].Group)
}

// Normalize returns keys deduplicated in catalog order, never nil, or ok=false when one isn't in the catalog.
func Normalize(keys []string) (out []string, ok bool) {
	out = []string{}
	for _, k := range keys {
		if !slices.ContainsFunc(Catalog, func(p Permission) bool { return p.Key == k }) {
			return nil, false
		}
	}
	for _, p := range Catalog {
		if slices.Contains(keys, p.Key) {
			out = append(out, p.Key)
		}
	}
	return out, true
}
