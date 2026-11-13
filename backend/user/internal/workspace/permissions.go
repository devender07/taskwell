package workspace

import (
	"slices"

	"github.com/devender07/taskwell/backend/shared/perms"
)

// Permission is one entry of the catalog (shared/perms).
type Permission = perms.Permission

const (
	PermWorkspaceEdit      = perms.WorkspaceEdit
	permWorkspaceDelete    = perms.WorkspaceDelete
	permWorkspacesCreate   = perms.WorkspacesCreate
	permMembersInvite      = perms.MembersInvite
	permMembersManage      = perms.MembersManage
	permRolesManage        = perms.RolesManage
	PermProjectsManage     = perms.ProjectsManage
	PermIssuesCreate       = perms.IssuesCreate
	PermIssuesEditAny      = perms.IssuesEditAny
	PermIssuesStatusAny    = perms.IssuesStatusAny
	PermWorkflowManage     = perms.WorkflowManage
	PermCommentsCreate     = perms.CommentsCreate
	PermCommentsManage     = perms.CommentsManage
	PermCyclesManage       = perms.CyclesManage
	PermAITestsRun         = perms.AITestsRun
	PermTestsManage        = perms.TestsManage
	PermAITestsManage      = perms.AITestsManage
	PermIntegrationsManage = perms.IntegrationsManage
	PermFeedbackManage     = perms.FeedbackManage
	PermAnalyticsView      = perms.AnalyticsView
	PermWorkspaceExport    = perms.WorkspaceExport
	PermAuditView          = perms.AuditView
)

var (
	catalog        = perms.Catalog
	allPermissions = perms.All
	guestDenied    = perms.GuestDenied
	normPerms      = perms.Normalize
)

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

// without returns the permissions in a that aren't in b, never nil.
func without(a, b []string) []string {
	return slices.DeleteFunc(append([]string{}, a...), func(k string) bool { return Has(b, k) })
}
