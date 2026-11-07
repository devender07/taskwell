package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// adminTools registers the tools for workspace administration and the caller's account. Personal API tokens have no
// tools: the API answers /api/me/tokens only to a cookie sign-in.
func (t *tools) adminTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "get_workspace", Annotations: readOnly,
		Description: "Read a workspace's name, URL slug, and your role and permissions in it."}, t.getWorkspace)
	mcp.AddTool(s, &mcp.Tool{Name: "update_workspace",
		Description: "Rename a workspace or change its URL slug. Only the fields you give change; a new slug changes every link."}, t.updateWorkspace)
	mcp.AddTool(s, &mcp.Tool{Name: "change_member_role",
		Description: "Give a workspace member another role (a role name from list_roles)."}, t.changeMemberRole)
	mcp.AddTool(s, &mcp.Tool{Name: "remove_member", Annotations: destructive,
		Description: "Remove a member from the workspace, or leave it by giving your own email."}, t.removeMember)
	mcp.AddTool(s, &mcp.Tool{Name: "list_invites", Annotations: readOnly,
		Description: "List a workspace's pending invites."}, t.listInvites)
	mcp.AddTool(s, &mcp.Tool{Name: "revoke_invite", Annotations: destructive,
		Description: "Cancel a pending invite, by the invited email. Its link stops working."}, t.revokeInvite)
	mcp.AddTool(s, &mcp.Tool{Name: "resend_invite",
		Description: "Email a pending invite again with a fresh link and expiry, by the invited email."}, t.resendInvite)
	mcp.AddTool(s, &mcp.Tool{Name: "my_invites", Annotations: readOnly,
		Description: "List your pending invites to workspaces you aren't in yet."}, t.myInvites)
	mcp.AddTool(s, &mcp.Tool{Name: "accept_invite",
		Description: "Join a workspace by accepting one of your invites (an id from my_invites)."}, t.acceptInvite)
	mcp.AddTool(s, &mcp.Tool{Name: "dismiss_invite",
		Description: "Say not now to one of your invites: it stays as a notification in a workspace you're in."}, t.dismissInvite)

	mcp.AddTool(s, &mcp.Tool{Name: "list_permissions", Annotations: readOnly,
		Description: "List the permission keys roles can have, with what each allows."}, t.listPermissions)
	mcp.AddTool(s, &mcp.Tool{Name: "list_roles", Annotations: readOnly,
		Description: "List a workspace's roles with their permissions and member counts."}, t.listRoles)
	mcp.AddTool(s, &mcp.Tool{Name: "create_role",
		Description: "Create a role with permission keys from list_permissions. You can only grant permissions you have."}, t.createRole)
	mcp.AddTool(s, &mcp.Tool{Name: "update_role",
		Description: "Rename a role or replace its permissions. Only the fields you give change; built-in roles keep their name."}, t.updateRole)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_role", Annotations: destructive,
		Description: "Delete a custom role. Built-in roles can't be deleted."}, t.deleteRole)

	mcp.AddTool(s, &mcp.Tool{Name: "update_status",
		Description: "Rename an issue status or change its category or status type. Only the fields you give change."}, t.updateStatus)
	mcp.AddTool(s, &mcp.Tool{Name: "reorder_statuses",
		Description: "Put a workspace's statuses in a new order. Give every status name once."}, t.reorderStatuses)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_status", Annotations: destructive,
		Description: "Delete an issue status. If issues use it, give move_to to move them to another status first."}, t.deleteStatus)
	mcp.AddTool(s, &mcp.Tool{Name: "list_status_types", Annotations: readOnly,
		Description: "List a workspace's status types: custom kinds of status with their own icon and color."}, t.listStatusTypes)
	mcp.AddTool(s, &mcp.Tool{Name: "create_status_type",
		Description: "Create a status type. Its statuses count as its category and show its icon and color."}, t.createStatusType)
	mcp.AddTool(s, &mcp.Tool{Name: "update_status_type",
		Description: "Change a status type. Only the fields you give change."}, t.updateStatusType)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_status_type", Annotations: destructive,
		Description: "Delete a status type; its statuses go back to their category's look."}, t.deleteStatusType)
	mcp.AddTool(s, &mcp.Tool{Name: "update_label",
		Description: "Rename or recolor an issue label. Only the fields you give change."}, t.updateLabel)

	mcp.AddTool(s, &mcp.Tool{Name: "list_webhooks", Annotations: readOnly,
		Description: "List a workspace's outbound webhooks with their events and last delivery."}, t.listWebhooks)
	mcp.AddTool(s, &mcp.Tool{Name: "create_webhook",
		Description: "Create a webhook that POSTs signed JSON to an https URL on the events given. The answer has its " +
			"signing secret, shown only this once: pass it on to whoever verifies the deliveries."}, t.createWebhook)
	mcp.AddTool(s, &mcp.Tool{Name: "update_webhook",
		Description: "Change a webhook's URL or events, or pause it (active false). Only the fields you give change."}, t.updateWebhook)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_webhook", Annotations: destructive, Description: "Delete a webhook."}, t.deleteWebhook)
	mcp.AddTool(s, &mcp.Tool{Name: "test_webhook",
		Description: "Queue a ping delivery to a webhook; see it with list_webhook_deliveries."}, t.testWebhook)
	mcp.AddTool(s, &mcp.Tool{Name: "list_webhook_deliveries", Annotations: readOnly,
		Description: "List a webhook's recent deliveries, newest first, with status, response and payload."}, t.listWebhookDeliveries)
	mcp.AddTool(s, &mcp.Tool{Name: "redeliver_webhook",
		Description: "Send one of a webhook's deliveries again."}, t.redeliverWebhook)
	mcp.AddTool(s, &mcp.Tool{Name: "roll_webhook_secret", Annotations: destructive,
		Description: "Replace a webhook's signing secret. The old one stops working; the new one is shown only this once."}, t.rollWebhookSecret)

	mcp.AddTool(s, &mcp.Tool{Name: "list_integrations", Annotations: readOnly,
		Description: "List the GitHub and GitLab integrations: connected, webhook URL and the statuses pull requests move issues to."},
		t.listIntegrations)
	mcp.AddTool(s, &mcp.Tool{Name: "connect_integration",
		Description: "Connect GitHub or GitLab. The answer has the webhook URL and secret to paste into the repository's settings, " +
			"shown only this once. Connecting again replaces them and the old ones stop working."}, t.connectIntegration)
	mcp.AddTool(s, &mcp.Tool{Name: "update_integration",
		Description: "Set the statuses an issue moves to when a linked pull request opens or merges. Empty leaves it unmoved."},
		t.updateIntegration)
	mcp.AddTool(s, &mcp.Tool{Name: "disconnect_integration", Annotations: destructive,
		Description: "Disconnect GitHub or GitLab; its webhook stops working."}, t.disconnectIntegration)

	mcp.AddTool(s, &mcp.Tool{Name: "import_csv",
		Description: "Import issues into a project from CSV text (Linear, Jira or any file with a Title or Summary column; " +
			"up to 10 MB and 5,000 rows). Use dry_run first to preview how statuses, labels and assignees map."}, t.importCSV)
	mcp.AddTool(s, &mcp.Tool{Name: "import_github",
		Description: "Import a GitHub repository's issues into a project. Use dry_run first to preview the mapping."}, t.importGitHub)

	mcp.AddTool(s, &mcp.Tool{Name: "get_profile", Annotations: readOnly,
		Description: "Read your Taskwell profile: name and email."}, t.getProfile)
	mcp.AddTool(s, &mcp.Tool{Name: "update_profile", Description: "Change your display name."}, t.updateProfile)
	mcp.AddTool(s, &mcp.Tool{Name: "get_notification_settings", Annotations: readOnly,
		Description: "Read your notification email settings."}, t.getNotificationSettings)
	mcp.AddTool(s, &mcp.Tool{Name: "update_notification_settings",
		Description: "Change your notification email settings. Only the fields you give change."}, t.updateNotificationSettings)
}

// --- inputs ---

type updateWorkspaceIn struct {
	Workspace string  `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Name      *string `json:"name,omitempty"`
	Slug      *string `json:"slug,omitempty" jsonschema:"new URL slug: 3–32 lowercase letters, numbers and single hyphens"`
}

type memberRoleIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Member    string `json:"member" jsonschema:"member's email or name"`
	Role      string `json:"role" jsonschema:"role name, from list_roles"`
}

type memberIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Member    string `json:"member" jsonschema:"member's email or name"`
}

type inviteEmailIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Email     string `json:"email" jsonschema:"the invited email, from list_invites"`
}

type myInviteIn struct {
	InviteID string `json:"invite_id" jsonschema:"from my_invites"`
}

type dismissInviteIn struct {
	InviteID  string `json:"invite_id" jsonschema:"from my_invites"`
	Workspace string `json:"workspace" jsonschema:"slug of a workspace you're in, where the invite stays as a notification"`
}

type createRoleIn struct {
	Workspace   string   `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions,omitempty" jsonschema:"permission keys, from list_permissions"`
}

type updateRoleIn struct {
	Workspace   string   `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Role        string   `json:"role" jsonschema:"the role's current name, from list_roles"`
	Name        *string  `json:"name,omitempty" jsonschema:"new name"`
	Permissions []string `json:"permissions,omitempty" jsonschema:"permission keys replacing the role's; [] removes them all"`
}

type roleIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Role      string `json:"role" jsonschema:"role name, from list_roles"`
}

type updateStatusIn struct {
	Workspace string  `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Status    string  `json:"status" jsonschema:"the status's current name, from workspace_options"`
	Name      *string `json:"name,omitempty" jsonschema:"new name"`
	Category  *string `json:"category,omitempty" jsonschema:"backlog, unstarted, started, completed or canceled; removes its status type"`
	Type      *string `json:"type,omitempty" jsonschema:"status type name, from list_status_types; empty removes it"`
}

type reorderStatusesIn struct {
	Workspace string   `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Statuses  []string `json:"statuses" jsonschema:"every status name once, in the new order"`
}

type deleteStatusIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Status    string `json:"status" jsonschema:"status name"`
	MoveTo    string `json:"move_to,omitempty" jsonschema:"status name to move its issues to"`
}

type createTypeIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Name      string `json:"name"`
	Category  string `json:"category" jsonschema:"how its statuses count: backlog, unstarted, started, completed or canceled"`
	Icon      string `json:"icon" jsonschema:"circle, circle-dashed, circle-half, circle-check, circle-x, eye, refresh, pause, clock, flag, star, bug, rocket, alert, lock or message"`
	Color     string `json:"color,omitempty" jsonschema:"hex color such as #5e6ad2; the category's when left out"`
}

type updateTypeIn struct {
	Workspace string  `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Type      string  `json:"type" jsonschema:"the status type's current name, from list_status_types"`
	Name      *string `json:"name,omitempty" jsonschema:"new name"`
	Category  *string `json:"category,omitempty" jsonschema:"backlog, unstarted, started, completed or canceled"`
	Icon      *string `json:"icon,omitempty" jsonschema:"an icon key, as create_status_type lists"`
	Color     *string `json:"color,omitempty" jsonschema:"hex color such as #5e6ad2; empty goes back to the category's"`
}

type typeIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Type      string `json:"type" jsonschema:"status type name, from list_status_types"`
}

type updateLabelIn struct {
	Workspace string  `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Label     string  `json:"label" jsonschema:"the label's current name"`
	Name      *string `json:"name,omitempty" jsonschema:"new name"`
	Color     *string `json:"color,omitempty" jsonschema:"hex color such as #5e6ad2"`
}

type createWebhookIn struct {
	Workspace string   `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	URL       string   `json:"url" jsonschema:"https URL that receives the POSTs"`
	Events    []string `json:"events" jsonschema:"issue.created, issue.updated, issue.deleted and/or comment.created"`
}

type updateWebhookIn struct {
	Workspace string   `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	ID        string   `json:"id" jsonschema:"webhook id, from list_webhooks"`
	URL       *string  `json:"url,omitempty"`
	Events    []string `json:"events,omitempty" jsonschema:"replaces its events"`
	Active    *bool    `json:"active,omitempty" jsonschema:"false pauses deliveries"`
}

type webhookIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	ID        string `json:"id" jsonschema:"webhook id, from list_webhooks"`
}

type redeliverIn struct {
	Workspace  string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	ID         string `json:"id" jsonschema:"webhook id, from list_webhooks"`
	DeliveryID string `json:"delivery_id" jsonschema:"from list_webhook_deliveries"`
}

type providerIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Provider  string `json:"provider" jsonschema:"github or gitlab"`
}

type integrationSettingsIn struct {
	Workspace   string  `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Provider    string  `json:"provider" jsonschema:"github or gitlab"`
	OpenStatus  *string `json:"open_status,omitempty" jsonschema:"status name an issue moves to when its pull request opens; empty clears it"`
	MergeStatus *string `json:"merge_status,omitempty" jsonschema:"status name an issue moves to when its pull request merges; empty clears it"`
}

type importCSVIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Project   string `json:"project" jsonschema:"project key, such as ENG"`
	CSV       string `json:"csv" jsonschema:"the CSV file's text, header row first"`
	DryRun    bool   `json:"dry_run,omitempty" jsonschema:"preview the import without creating issues"`
}

type importGitHubIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Project   string `json:"project" jsonschema:"project key, such as ENG"`
	Repo      string `json:"repo" jsonschema:"owner/name or a github.com URL"`
	Token     string `json:"token,omitempty" jsonschema:"GitHub token for a private repository; used once, never stored"`
	Closed    bool   `json:"closed,omitempty" jsonschema:"include closed issues"`
	DryRun    bool   `json:"dry_run,omitempty" jsonschema:"preview the import without creating issues"`
}

type profileIn struct {
	Name string `json:"name"`
}

type notificationSettingsIn struct {
	Email       *string `json:"email,omitempty" jsonschema:"instant, digest or off"`
	Mentions    *bool   `json:"mentions,omitempty"`
	Assignments *bool   `json:"assignments,omitempty"`
	Status      *bool   `json:"status,omitempty" jsonschema:"status changes on issues you watch"`
	Comments    *bool   `json:"comments,omitempty"`
	Chat        *bool   `json:"chat,omitempty" jsonschema:"chat mentions and direct messages"`
	Weekly      *bool   `json:"weekly,omitempty" jsonschema:"the Monday summary email"`
	DueDates    *bool   `json:"due_dates,omitempty" jsonschema:"due-soon and overdue reminders"`
	Timezone    *string `json:"timezone,omitempty" jsonschema:"IANA name such as Europe/Berlin, for the digest; empty is UTC"`
}

// --- API shapes ---

type wsRole struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Builtin     *string  `json:"builtin"`
	Permissions []string `json:"permissions"`
	MemberCount int      `json:"member_count"`
}

type pendingInvite struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	InvitedBy string `json:"invited_by"`
	ExpiresAt string `json:"expires_at"`
	Role      label  `json:"role"`
}

// statusRow is a status as the statuses endpoints answer, with its type.
type statusRow struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Type     *label `json:"type"`
}

type statusKind struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Category string  `json:"category"`
	Icon     string  `json:"icon"`
	Color    *string `json:"color"`
}

type colorLabel struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// --- workspace and members ---

func (t *tools) getWorkspace(ctx context.Context, _ *mcp.CallToolRequest, in wsIn) (*mcp.CallToolResult, any, error) {
	return t.relay(ctx, ws(in.Workspace), nil)
}

func (t *tools) updateWorkspace(ctx context.Context, _ *mcp.CallToolRequest, in updateWorkspaceIn) (*mcp.CallToolResult, any, error) {
	// The API replaces name and slug together, so the one left out is read first.
	var w struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := t.api.get(ctx, ws(in.Workspace), nil, &w); err != nil {
		return nil, nil, err
	}
	if in.Name != nil {
		w.Name = *in.Name
	}
	if in.Slug != nil {
		w.Slug = *in.Slug
	}
	return t.relay(ctx, ws(in.Workspace), url.Values{"name": {w.Name}, "slug": {w.Slug}})
}

func (t *tools) changeMemberRole(ctx context.Context, _ *mcp.CallToolRequest, in memberRoleIn) (*mcp.CallToolResult, any, error) {
	m, err := t.member(ctx, in.Workspace, in.Member)
	if err != nil {
		return nil, nil, err
	}
	r, err := t.role(ctx, in.Workspace, in.Role)
	if err != nil {
		return nil, nil, err
	}
	return t.relay(ctx, ws(in.Workspace, "members", m.ID), url.Values{"role_id": {r.ID}})
}

func (t *tools) removeMember(ctx context.Context, _ *mcp.CallToolRequest, in memberIn) (*mcp.CallToolResult, any, error) {
	m, err := t.member(ctx, in.Workspace, in.Member)
	if err != nil {
		return nil, nil, err
	}
	if err := t.api.del(ctx, ws(in.Workspace, "members", m.ID), nil); err != nil {
		return nil, nil, err
	}
	return result(map[string]string{"removed": m.Email})
}

func (t *tools) invites(ctx context.Context, slug string) ([]pendingInvite, error) {
	var res struct {
		Invites []pendingInvite `json:"invites"`
	}
	return res.Invites, t.api.get(ctx, ws(slug, "members"), nil, &res)
}

// invite finds the workspace's pending invite to the email.
func (t *tools) invite(ctx context.Context, slug, email string) (pendingInvite, error) {
	is, err := t.invites(ctx, slug)
	if err != nil {
		return pendingInvite{}, err
	}
	return find("invite", email, is, func(i pendingInvite) []string { return []string{i.Email} },
		func(i pendingInvite) string { return i.Email })
}

func (t *tools) listInvites(ctx context.Context, _ *mcp.CallToolRequest, in wsIn) (*mcp.CallToolResult, any, error) {
	is, err := t.invites(ctx, in.Workspace)
	if err != nil {
		return nil, nil, err
	}
	type inviteOut struct {
		Email     string `json:"email"`
		Role      string `json:"role"`
		InvitedBy string `json:"invited_by,omitempty"`
		ExpiresAt string `json:"expires_at"`
	}
	o := make([]inviteOut, len(is))
	for k, i := range is {
		o[k] = inviteOut{i.Email, i.Role.Name, i.InvitedBy, i.ExpiresAt}
	}
	return result(o)
}

func (t *tools) revokeInvite(ctx context.Context, _ *mcp.CallToolRequest, in inviteEmailIn) (*mcp.CallToolResult, any, error) {
	i, err := t.invite(ctx, in.Workspace, in.Email)
	if err != nil {
		return nil, nil, err
	}
	if err := t.api.del(ctx, ws(in.Workspace, "invites", i.ID), nil); err != nil {
		return nil, nil, err
	}
	return result(map[string]string{"revoked": i.Email})
}

func (t *tools) resendInvite(ctx context.Context, _ *mcp.CallToolRequest, in inviteEmailIn) (*mcp.CallToolResult, any, error) {
	i, err := t.invite(ctx, in.Workspace, in.Email)
	if err != nil {
		return nil, nil, err
	}
	return t.relay(ctx, ws(in.Workspace, "invites", i.ID, "resend"), url.Values{})
}

func (t *tools) myInvites(ctx context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	return t.relay(ctx, "/api/me/invites", nil)
}

func (t *tools) acceptInvite(ctx context.Context, _ *mcp.CallToolRequest, in myInviteIn) (*mcp.CallToolResult, any, error) {
	return t.relay(ctx, "/api/me/invites/"+url.PathEscape(in.InviteID)+"/accept", url.Values{})
}

func (t *tools) dismissInvite(ctx context.Context, _ *mcp.CallToolRequest, in dismissInviteIn) (*mcp.CallToolResult, any, error) {
	path := "/api/me/invites/" + url.PathEscape(in.InviteID) + "/dismiss"
	if err := t.api.post(ctx, path, url.Values{"workspace": {in.Workspace}}, nil); err != nil {
		return nil, nil, err
	}
	return result(map[string]string{"dismissed": in.InviteID})
}

// --- roles ---

func (t *tools) roles(ctx context.Context, slug string) ([]wsRole, error) {
	var rs []wsRole
	return rs, t.api.get(ctx, ws(slug, "roles"), nil, &rs)
}

// role finds the workspace's role with the name.
func (t *tools) role(ctx context.Context, slug, name string) (wsRole, error) {
	rs, err := t.roles(ctx, slug)
	if err != nil {
		return wsRole{}, err
	}
	return find("role", name, rs, func(r wsRole) []string { return []string{r.Name} }, func(r wsRole) string { return r.Name })
}

func (t *tools) listPermissions(ctx context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	return t.relay(ctx, "/api/permissions", nil)
}

func (t *tools) listRoles(ctx context.Context, _ *mcp.CallToolRequest, in wsIn) (*mcp.CallToolResult, any, error) {
	rs, err := t.roles(ctx, in.Workspace)
	if err != nil {
		return nil, nil, err
	}
	for k := range rs {
		rs[k].ID = ""
	}
	return result(rs)
}

func (t *tools) createRole(ctx context.Context, _ *mcp.CallToolRequest, in createRoleIn) (*mcp.CallToolResult, any, error) {
	return t.relay(ctx, ws(in.Workspace, "roles"), url.Values{"name": {in.Name}, "permissions": in.Permissions})
}

func (t *tools) updateRole(ctx context.Context, _ *mcp.CallToolRequest, in updateRoleIn) (*mcp.CallToolResult, any, error) {
	// The API replaces name and permissions together, so the ones left out are the role's own.
	r, err := t.role(ctx, in.Workspace, in.Role)
	if err != nil {
		return nil, nil, err
	}
	if in.Name != nil {
		r.Name = *in.Name
	}
	if in.Permissions != nil {
		r.Permissions = in.Permissions
	}
	return t.relay(ctx, ws(in.Workspace, "roles", r.ID), url.Values{"name": {r.Name}, "permissions": r.Permissions})
}

func (t *tools) deleteRole(ctx context.Context, _ *mcp.CallToolRequest, in roleIn) (*mcp.CallToolResult, any, error) {
	r, err := t.role(ctx, in.Workspace, in.Role)
	if err != nil {
		return nil, nil, err
	}
	if err := t.api.del(ctx, ws(in.Workspace, "roles", r.ID), nil); err != nil {
		return nil, nil, err
	}
	return result(map[string]string{"deleted": r.Name})
}

// --- statuses, status types and labels ---

func (t *tools) statusRows(ctx context.Context, slug string) ([]statusRow, error) {
	var ss []statusRow
	return ss, t.api.get(ctx, ws(slug, "statuses"), nil, &ss)
}

func findStatus(want string, ss []statusRow) (statusRow, error) {
	return find("status", want, ss, func(s statusRow) []string { return []string{s.Name} }, func(s statusRow) string { return s.Name })
}

func (t *tools) statusKinds(ctx context.Context, slug string) ([]statusKind, error) {
	var ts []statusKind
	return ts, t.api.get(ctx, ws(slug, "status-types"), nil, &ts)
}

func findKind(want string, ts []statusKind) (statusKind, error) {
	return find("status type", want, ts, func(k statusKind) []string { return []string{k.Name} }, func(k statusKind) string { return k.Name })
}

func (t *tools) updateStatus(ctx context.Context, _ *mcp.CallToolRequest, in updateStatusIn) (*mcp.CallToolResult, any, error) {
	ss, err := t.statusRows(ctx, in.Workspace)
	if err != nil {
		return nil, nil, err
	}
	s, err := findStatus(in.Status, ss)
	if err != nil {
		return nil, nil, err
	}
	// The API replaces name and category or type together: a type_id wins, else the category stands alone.
	form := url.Values{"name": {s.Name}, "category": {s.Category}}
	if in.Name != nil {
		form.Set("name", *in.Name)
	}
	switch {
	case in.Type != nil && strings.TrimSpace(*in.Type) != "":
		ts, err := t.statusKinds(ctx, in.Workspace)
		if err != nil {
			return nil, nil, err
		}
		k, err := findKind(*in.Type, ts)
		if err != nil {
			return nil, nil, err
		}
		form.Set("type_id", k.ID)
	case in.Category != nil:
		form.Set("category", strings.ToLower(strings.TrimSpace(*in.Category)))
	case in.Type == nil && s.Type != nil:
		form.Set("type_id", s.Type.ID)
	}
	return t.relay(ctx, ws(in.Workspace, "statuses", s.ID), form)
}

func (t *tools) reorderStatuses(ctx context.Context, _ *mcp.CallToolRequest, in reorderStatusesIn) (*mcp.CallToolResult, any, error) {
	ss, err := t.statusRows(ctx, in.Workspace)
	if err != nil {
		return nil, nil, err
	}
	form := url.Values{}
	for _, n := range in.Statuses {
		s, err := findStatus(n, ss)
		if err != nil {
			return nil, nil, err
		}
		form.Add("ids", s.ID)
	}
	if err := t.api.post(ctx, ws(in.Workspace, "statuses", "order"), form, &ss); err != nil {
		return nil, nil, err
	}
	names := make([]string, len(ss))
	for k, s := range ss {
		names[k] = s.Name
	}
	return result(names)
}

func (t *tools) deleteStatus(ctx context.Context, _ *mcp.CallToolRequest, in deleteStatusIn) (*mcp.CallToolResult, any, error) {
	ss, err := t.statusRows(ctx, in.Workspace)
	if err != nil {
		return nil, nil, err
	}
	s, err := findStatus(in.Status, ss)
	if err != nil {
		return nil, nil, err
	}
	path := ws(in.Workspace, "statuses", s.ID)
	if strings.TrimSpace(in.MoveTo) != "" {
		to, err := findStatus(in.MoveTo, ss)
		if err != nil {
			return nil, nil, err
		}
		path += "?" + url.Values{"move_to": {to.ID}}.Encode()
	}
	if err := t.api.del(ctx, path, nil); err != nil {
		return nil, nil, err
	}
	return result(map[string]string{"deleted": s.Name})
}

func (t *tools) listStatusTypes(ctx context.Context, _ *mcp.CallToolRequest, in wsIn) (*mcp.CallToolResult, any, error) {
	return t.relay(ctx, ws(in.Workspace, "status-types"), nil)
}

func (t *tools) createStatusType(ctx context.Context, _ *mcp.CallToolRequest, in createTypeIn) (*mcp.CallToolResult, any, error) {
	form := url.Values{"name": {in.Name}, "category": {in.Category}, "icon": {in.Icon}, "color": {in.Color}}
	return t.relay(ctx, ws(in.Workspace, "status-types"), form)
}

func (t *tools) updateStatusType(ctx context.Context, _ *mcp.CallToolRequest, in updateTypeIn) (*mcp.CallToolResult, any, error) {
	ts, err := t.statusKinds(ctx, in.Workspace)
	if err != nil {
		return nil, nil, err
	}
	k, err := findKind(in.Type, ts)
	if err != nil {
		return nil, nil, err
	}
	form := url.Values{"name": {k.Name}, "category": {k.Category}, "icon": {k.Icon}}
	if k.Color != nil {
		form.Set("color", *k.Color)
	}
	for f, v := range map[string]*string{"name": in.Name, "category": in.Category, "icon": in.Icon, "color": in.Color} {
		if v != nil {
			form.Set(f, *v)
		}
	}
	return t.relay(ctx, ws(in.Workspace, "status-types", k.ID), form)
}

func (t *tools) deleteStatusType(ctx context.Context, _ *mcp.CallToolRequest, in typeIn) (*mcp.CallToolResult, any, error) {
	ts, err := t.statusKinds(ctx, in.Workspace)
	if err != nil {
		return nil, nil, err
	}
	k, err := findKind(in.Type, ts)
	if err != nil {
		return nil, nil, err
	}
	if err := t.api.del(ctx, ws(in.Workspace, "status-types", k.ID), nil); err != nil {
		return nil, nil, err
	}
	return result(map[string]string{"deleted": k.Name})
}

func (t *tools) updateLabel(ctx context.Context, _ *mcp.CallToolRequest, in updateLabelIn) (*mcp.CallToolResult, any, error) {
	var ls []colorLabel
	if err := t.api.get(ctx, ws(in.Workspace, "labels"), nil, &ls); err != nil {
		return nil, nil, err
	}
	l, err := find("label", in.Label, ls, func(l colorLabel) []string { return []string{l.Name} }, func(l colorLabel) string { return l.Name })
	if err != nil {
		return nil, nil, err
	}
	if in.Name != nil {
		l.Name = *in.Name
	}
	if in.Color != nil {
		l.Color = *in.Color
	}
	return t.relay(ctx, ws(in.Workspace, "labels", l.ID), url.Values{"name": {l.Name}, "color": {l.Color}})
}

// --- webhooks ---

func (t *tools) listWebhooks(ctx context.Context, _ *mcp.CallToolRequest, in wsIn) (*mcp.CallToolResult, any, error) {
	return t.relay(ctx, ws(in.Workspace, "webhooks"), nil)
}

func (t *tools) createWebhook(ctx context.Context, _ *mcp.CallToolRequest, in createWebhookIn) (*mcp.CallToolResult, any, error) {
	return t.relay(ctx, ws(in.Workspace, "webhooks"), url.Values{"url": {in.URL}, "events": in.Events})
}

func (t *tools) updateWebhook(ctx context.Context, _ *mcp.CallToolRequest, in updateWebhookIn) (*mcp.CallToolResult, any, error) {
	form := url.Values{}
	if in.URL != nil {
		form.Set("url", *in.URL)
	}
	if in.Events != nil {
		form["events"] = append([]string{""}, in.Events...) // present even when empty, so the API says to choose one
	}
	if in.Active != nil {
		form.Set("active", strconv.FormatBool(*in.Active))
	}
	if len(form) == 0 {
		return nil, nil, errors.New("Give at least one field to change.")
	}
	return t.relay(ctx, ws(in.Workspace, "webhooks", in.ID), form)
}

func (t *tools) deleteWebhook(ctx context.Context, _ *mcp.CallToolRequest, in webhookIn) (*mcp.CallToolResult, any, error) {
	if err := t.api.del(ctx, ws(in.Workspace, "webhooks", in.ID), nil); err != nil {
		return nil, nil, err
	}
	return result(map[string]string{"deleted": in.ID})
}

func (t *tools) testWebhook(ctx context.Context, _ *mcp.CallToolRequest, in webhookIn) (*mcp.CallToolResult, any, error) {
	if err := t.api.post(ctx, ws(in.Workspace, "webhooks", in.ID, "test"), url.Values{}, nil); err != nil {
		return nil, nil, err
	}
	return result(map[string]bool{"queued": true})
}

func (t *tools) listWebhookDeliveries(ctx context.Context, _ *mcp.CallToolRequest, in webhookIn) (*mcp.CallToolResult, any, error) {
	return t.relay(ctx, ws(in.Workspace, "webhooks", in.ID, "deliveries"), nil)
}

func (t *tools) redeliverWebhook(ctx context.Context, _ *mcp.CallToolRequest, in redeliverIn) (*mcp.CallToolResult, any, error) {
	path := ws(in.Workspace, "webhooks", in.ID, "deliveries", in.DeliveryID, "redeliver")
	if err := t.api.post(ctx, path, url.Values{}, nil); err != nil {
		return nil, nil, err
	}
	return result(map[string]bool{"queued": true})
}

func (t *tools) rollWebhookSecret(ctx context.Context, _ *mcp.CallToolRequest, in webhookIn) (*mcp.CallToolResult, any, error) {
	return t.relay(ctx, ws(in.Workspace, "webhooks", in.ID, "secret"), url.Values{})
}

// --- integrations ---

func (t *tools) listIntegrations(ctx context.Context, _ *mcp.CallToolRequest, in wsIn) (*mcp.CallToolResult, any, error) {
	return t.relay(ctx, ws(in.Workspace, "integrations"), nil)
}

func gitProvider(p string) string { return strings.ToLower(strings.TrimSpace(p)) }

func (t *tools) connectIntegration(ctx context.Context, _ *mcp.CallToolRequest, in providerIn) (*mcp.CallToolResult, any, error) {
	return t.relay(ctx, ws(in.Workspace, "integrations", gitProvider(in.Provider)), url.Values{})
}

func (t *tools) updateIntegration(ctx context.Context, _ *mcp.CallToolRequest, in integrationSettingsIn) (*mcp.CallToolResult, any, error) {
	form := url.Values{}
	var ss []statusRow
	for f, v := range map[string]*string{"open_status": in.OpenStatus, "merge_status": in.MergeStatus} {
		if v == nil {
			continue
		}
		form.Set(f, "")
		if strings.TrimSpace(*v) == "" {
			continue
		}
		if ss == nil {
			var err error
			if ss, err = t.statusRows(ctx, in.Workspace); err != nil {
				return nil, nil, err
			}
		}
		s, err := findStatus(*v, ss)
		if err != nil {
			return nil, nil, err
		}
		form.Set(f, s.ID)
	}
	if len(form) == 0 {
		return nil, nil, errors.New("Give open_status or merge_status.")
	}
	return t.relay(ctx, ws(in.Workspace, "integrations", gitProvider(in.Provider), "settings"), form)
}

func (t *tools) disconnectIntegration(ctx context.Context, _ *mcp.CallToolRequest, in providerIn) (*mcp.CallToolResult, any, error) {
	if err := t.api.del(ctx, ws(in.Workspace, "integrations", gitProvider(in.Provider)), nil); err != nil {
		return nil, nil, err
	}
	return result(map[string]string{"disconnected": gitProvider(in.Provider)})
}

// --- import ---

const maxImportCSV = 10 << 20 // the API's cap

func (t *tools) importCSV(ctx context.Context, _ *mcp.CallToolRequest, in importCSVIn) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.CSV) == "" {
		return nil, nil, errors.New("Give the CSV text.")
	}
	if len(in.CSV) > maxImportCSV {
		return nil, nil, errors.New("The file is over 10 MB.")
	}
	fields := url.Values{"project": {in.Project}}
	if in.DryRun {
		fields.Set("dry_run", "1")
	}
	var out json.RawMessage
	if err := t.api.upload(ctx, ws(in.Workspace, "import", "csv"), fields, "import.csv", []byte(in.CSV), &out); err != nil {
		return nil, nil, err
	}
	return result(out)
}

func (t *tools) importGitHub(ctx context.Context, _ *mcp.CallToolRequest, in importGitHubIn) (*mcp.CallToolResult, any, error) {
	form := url.Values{"project": {in.Project}, "repo": {in.Repo}, "token": {in.Token}}
	if in.Closed {
		form.Set("closed", "1")
	}
	if in.DryRun {
		form.Set("dry_run", "1")
	}
	return t.relay(ctx, ws(in.Workspace, "import", "github"), form)
}

// --- account ---

func (t *tools) getProfile(ctx context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	var p struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := t.api.get(ctx, "/api/me", nil, &p); err != nil {
		return nil, nil, err
	}
	return result(p)
}

func (t *tools) updateProfile(ctx context.Context, _ *mcp.CallToolRequest, in profileIn) (*mcp.CallToolResult, any, error) {
	var p struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := t.api.post(ctx, "/api/me", url.Values{"name": {in.Name}}, &p); err != nil {
		return nil, nil, err
	}
	return result(p)
}

func (t *tools) getNotificationSettings(ctx context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	return t.relay(ctx, "/api/me/notification-settings", nil)
}

func (t *tools) updateNotificationSettings(ctx context.Context, _ *mcp.CallToolRequest, in notificationSettingsIn) (*mcp.CallToolResult, any, error) {
	form := url.Values{}
	for f, v := range map[string]*string{"email": in.Email, "timezone": in.Timezone} {
		if v != nil {
			form.Set(f, strings.TrimSpace(*v))
		}
	}
	for f, v := range map[string]*bool{"mentions": in.Mentions, "assignments": in.Assignments, "status": in.Status,
		"comments": in.Comments, "chat": in.Chat, "weekly": in.Weekly, "due_dates": in.DueDates} {
		if v != nil {
			form.Set(f, strconv.FormatBool(*v))
		}
	}
	if len(form) == 0 {
		return nil, nil, errors.New("Give at least one setting to change.")
	}
	return t.relay(ctx, "/api/me/notification-settings", form)
}

// relay answers with the API's JSON as it is: GET path when form is nil, else POST form to it.
func (t *tools) relay(ctx context.Context, path string, form url.Values) (*mcp.CallToolResult, any, error) {
	var raw json.RawMessage
	var err error
	if form == nil {
		err = t.api.get(ctx, path, nil, &raw)
	} else {
		err = t.api.post(ctx, path, form, &raw)
	}
	if err != nil {
		return nil, nil, err
	}
	if len(raw) == 0 {
		raw = json.RawMessage(`{"done":true}`)
	}
	return result(raw)
}
