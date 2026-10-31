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
	permWorkspaceEdit   = "workspace.edit"
	permWorkspaceDelete = "workspace.delete"
	permMembersInvite   = "members.invite"
	permMembersManage   = "members.manage"
	permRolesManage     = "roles.manage"

	// PermProjectsManage gates creating, editing and archiving projects.
	PermProjectsManage = "projects.manage"
	// PermIssuesCreate gates creating issues.
	PermIssuesCreate = "issues.create"
	// PermIssuesEditAny lets a member edit and delete issues they neither created nor are assigned.
	PermIssuesEditAny = "issues.edit_any"
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
	// PermIntegrationsManage gates connecting GitHub and GitLab webhooks.
	PermIntegrationsManage = "integrations.manage"
)

// catalog is every permission in display order.
var catalog = []Permission{
	{permWorkspaceEdit, "Workspace", "Edit workspace name and URL", true},
	{permWorkspaceDelete, "Workspace", "Delete the workspace", true},
	{PermIntegrationsManage, "Workspace", "Connect GitHub and GitLab", true},
	{permMembersInvite, "Members", "Invite people, resend and cancel invites", true},
	{permMembersManage, "Members", "Change members' roles and remove members", true},
	{permRolesManage, "Members", "Create, edit and delete roles", true},
	{PermProjectsManage, "Projects", "Create, edit and archive projects", true},
	{PermIssuesCreate, "Issues", "Create issues", true},
	{PermIssuesEditAny, "Issues", "Edit and delete anyone's issues", true},
	{PermWorkflowManage, "Issues", "Manage statuses and labels", true},
	{PermCyclesManage, "Cycles", "Create, edit and delete cycles", true},
	{PermCommentsCreate, "Comments", "Comment on issues", true},
	{PermCommentsManage, "Comments", "Edit and delete anyone's comments", true},
	{PermAITestsRun, "Testing", "Run AI tests on issues", true},
	{PermTestsManage, "Testing", "Edit test cases and run tests manually", true},
	{PermAITestsManage, "Testing", "Manage test sites and test accounts", true},
}

func allPermissions() []string {
	keys := make([]string, len(catalog))
	for i, p := range catalog {
		keys[i] = p.Key
	}
	return keys
}

// effective is a role's permissions: the whole catalog for Admin, the stored set otherwise.
func effective(builtin string, perms []string) []string {
	if builtin == roleAdmin {
		return allPermissions()
	}
	return perms
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
