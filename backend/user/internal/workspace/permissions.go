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
)

// catalog is every permission in display order.
var catalog = []Permission{
	{permWorkspaceEdit, "Workspace", "Edit workspace name and URL", true},
	{permWorkspaceDelete, "Workspace", "Delete the workspace", true},
	{permMembersInvite, "Members", "Invite people, resend and cancel invites", true},
	{permMembersManage, "Members", "Change members' roles and remove members", true},
	{permRolesManage, "Members", "Create, edit and delete roles", true},
	{PermProjectsManage, "Projects", "Create, edit and archive projects", true},
	{"issues.create", "Issues", "Create issues", false},
	{"issues.edit_any", "Issues", "Edit and delete anyone's issues", false},
	{"comments.create", "Comments", "Comment on issues", false},
	{"comments.manage", "Comments", "Edit and delete anyone's comments", false},
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
