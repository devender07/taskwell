package workspace

import "slices"

// Permission is one entry of the catalog; Active is false until the feature it gates ships.
type Permission struct {
	Key    string `json:"key"`
	Group  string `json:"group"`
	Label  string `json:"label"`
	Active bool   `json:"active"`
}

const (
	// PermWorkspaceEdit gates renaming the workspace; it also lets a member edit and delete others' shared views.
	PermWorkspaceEdit   = "workspace.edit"
	permWorkspaceDelete = "workspace.delete"
	// permWorkspacesCreate lets a member create new workspaces; checked in SQL by can_create_workspace.
	permWorkspacesCreate = "workspaces.create"
	permMembersInvite    = "members.invite"
	permMembersManage    = "members.manage"
	permRolesManage      = "roles.manage"

	// PermProjectsManage gates creating, editing and archiving projects.
	PermProjectsManage = "projects.manage"
	// PermIssuesCreate gates creating issues.
	PermIssuesCreate = "issues.create"
	// PermIssuesEditAny lets a member edit and delete issues they neither created nor are assigned.
	PermIssuesEditAny = "issues.edit_any"
	// PermIssuesStatusAny lets a member change the status of any issue they can see (QA reopening work), nothing else.
	PermIssuesStatusAny = "issues.status_any"
	// PermWorkflowManage gates creating, editing, reordering and deleting statuses and labels.
	PermWorkflowManage = "workflow.manage"
	// PermCommentsCreate gates commenting on issues and editing one's own comments.
	PermCommentsCreate = "comments.create"
	// PermCommentsManage lets a member edit and delete anyone's comments.
	PermCommentsManage = "comments.manage"
	// PermCyclesManage gates creating, editing, deleting and rolling over cycles.
	PermCyclesManage = "cycles.manage"
	// PermAITestsRun gates starting and cancelling AI test runs.
	PermAITestsRun = "ai_tests.run"
	// PermTestsManage gates editing an issue's test cases and running them by hand.
	PermTestsManage = "tests.manage"
	// PermAITestsManage gates the workspace's test sites and test accounts.
	PermAITestsManage = "ai_tests.manage"
	// PermIntegrationsManage gates connecting GitHub and GitLab and managing outbound webhooks.
	PermIntegrationsManage = "integrations.manage"
	// PermFeedbackManage gates the public feedback form and archiving and deleting feedback.
	PermFeedbackManage = "feedback.manage"
	// PermAnalyticsView lets a member see other members' analytics and the team page.
	PermAnalyticsView = "analytics.view"
	// PermWorkspaceExport lets a member download the workspace's data (docs/workspace-export-plan.md).
	PermWorkspaceExport = "workspace.export"
	// PermAuditView lets a member read the workspace audit log (docs/workspace-audit-plan.md).
	PermAuditView = "audit.view"
	// PermBillingManage lets a member change the workspace's plan and payment details (docs/billing-plan.md).
	PermBillingManage = "billing.manage"
)

// catalog is every permission in display order.
var catalog = []Permission{
	{PermWorkspaceEdit, "Workspace", "Edit workspace name and URL", true},
	{permWorkspaceDelete, "Workspace", "Delete the workspace", true},
	{permWorkspacesCreate, "Workspace", "Create new workspaces", true},
	{PermIntegrationsManage, "Workspace", "Manage GitHub, GitLab and webhooks", true},
	{PermWorkspaceExport, "Workspace", "Export workspace data", true},
	{PermAuditView, "Workspace", "See the audit log", true},
	{PermBillingManage, "Workspace", "Manage billing", true},
	{permMembersInvite, "Members", "Invite people, resend and cancel invites", true},
	{permMembersManage, "Members", "Change members' roles and remove members", true},
	{permRolesManage, "Members", "Create, edit and delete roles", true},
	{PermProjectsManage, "Projects", "Create, edit and archive projects", true},
	{PermIssuesCreate, "Issues", "Create issues", true},
	{PermIssuesEditAny, "Issues", "Edit and delete anyone's issues", true},
	{PermIssuesStatusAny, "Issues", "Change the status of anyone's issues", true},
	{PermWorkflowManage, "Issues", "Manage statuses and labels", true},
	{PermCyclesManage, "Cycles", "Create, edit and delete cycles", true},
	{PermCommentsCreate, "Comments", "Comment on issues", true},
	{PermCommentsManage, "Comments", "Edit and delete anyone's comments", true},
	{PermAITestsRun, "Testing", "Run AI tests on issues", true},
	{PermTestsManage, "Testing", "Edit test cases and run tests manually", true},
	{PermAITestsManage, "Testing", "Manage test sites and test accounts", true},
	{PermFeedbackManage, "Feedback", "Manage the feedback form, archive and delete feedback", true},
	{PermAnalyticsView, "Analytics", "See other members' analytics and the team page", true},
}

func allPermissions() []string {
	keys := make([]string, len(catalog))
	for i, p := range catalog {
		keys[i] = p.Key
	}
	return keys
}

// Effective is a role's permissions: the whole catalog for Admin, the stored set otherwise (never a guestDenied one
// for Guest).
func Effective(builtin string, perms []string) []string {
	switch builtin {
	case roleAdmin:
		return allPermissions()
	case roleGuest:
		return slices.DeleteFunc(slices.Clone(perms), guestDenied)
	}
	return perms
}

// guestDenied reports whether a Guest role may not hold the permission: none of the Workspace, Members and
// Analytics groups.
func guestDenied(key string) bool {
	i := slices.IndexFunc(catalog, func(p Permission) bool { return p.Key == key })
	return i >= 0 && slices.Contains([]string{"Workspace", "Members", "Analytics"}, catalog[i].Group)
}

// Has reports whether perms includes key.
func Has(perms []string, key string) bool { return slices.Contains(perms, key) }

// subset reports whether every permission in want is in have (the no-escalation rule).
func subset(want, have []string) bool {
	for _, k := range want {
		if !Has(have, k) {
			return false
		}
	}
	return true
}

// normPerms returns keys deduplicated in catalog order, or ok=false when one isn't in the catalog.
func normPerms(keys []string) (out []string, ok bool) {
	out = []string{}
	for _, k := range keys {
		if !slices.ContainsFunc(catalog, func(p Permission) bool { return p.Key == k }) {
			return nil, false
		}
	}
	for _, p := range catalog {
		if Has(keys, p.Key) {
			out = append(out, p.Key)
		}
	}
	return out, true
}

// without returns the permissions in a that aren't in b, never nil.
func without(a, b []string) []string {
	return slices.DeleteFunc(append([]string{}, a...), func(k string) bool { return Has(b, k) })
}
