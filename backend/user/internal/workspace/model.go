package workspace

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/db"
	"github.com/devender07/taskwell/backend/user/internal/plan"
	"github.com/devender07/taskwell/backend/user/internal/realtime"
	"github.com/devender07/taskwell/backend/user/internal/web"
	"github.com/devender07/taskwell/backend/user/internal/wsaudit"
)

// roleAdmin and roleGuest are the Admin and Guest roles' workspace_roles.builtin.
const (
	roleAdmin = "admin"
	roleGuest = "guest"
)

// memberDefaults are a new workspace's Member role permissions.
var memberDefaults = []string{"issues.create", "comments.create"}

var (
	errTaken     = errors.New("workspace slug taken")
	errNoCreate  = errors.New("not allowed to create workspaces")
	errLastAdmin = errors.New("last admin")
	errFull      = errors.New("workspace full")
	errNameTaken = errors.New("role name taken")
)

type RoleRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Workspace is a workspace as seen by one member: their role and its effective permissions. A Guest sees only the
// projects they're a member of and nothing workspace-wide (chat, feedback, analytics, settings).
type Workspace struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Slug        string   `json:"slug"`
	Role        RoleRef  `json:"role"`
	Permissions []string `json:"permissions"`
	Guest       bool     `json:"guest"`
}

// wsCols selects a Workspace from workspaces w and workspace_roles r; scan it with scanWorkspace.
const wsCols = `w.id::text, w.name, w.slug, r.id::text, r.name, coalesce(r.builtin, ''), r.permissions`

// wsFrom joins a member m to their workspace w and role r.
const wsFrom = `workspace_members m join workspaces w on w.id = m.workspace_id join workspace_roles r on r.id = m.role_id`

func scanWorkspace(row pgx.Row) (Workspace, error) {
	var ws Workspace
	var builtin string
	err := row.Scan(&ws.ID, &ws.Name, &ws.Slug, &ws.Role.ID, &ws.Role.Name, &builtin, &ws.Permissions)
	ws.Permissions, ws.Guest = Effective(builtin, ws.Permissions), builtin == roleGuest
	return ws, err
}

// model owns every query for workspaces, their roles and members.
type model struct{ db *pgxpool.Pool }

// list returns the user's workspaces, most recently opened first.
func (m *model) list(ctx context.Context, userID string) ([]Workspace, error) {
	rows, err := m.db.Query(ctx,
		`select `+wsCols+` from `+wsFrom+`
		  where m.user_id = $1
		  order by m.last_opened_at desc, w.created_at desc`, userID)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	ws, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Workspace, error) { return scanWorkspace(row) })
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	return ws, nil
}

// create inserts the workspace with its built-in roles and makes the user its admin. Returns errNoCreate without the
// permission, plan.Exceeded when the user already owns as many workspaces as their plan allows, errTaken on a
// duplicate slug.
func (m *model) create(ctx context.Context, userID, name, slug string) (Workspace, error) {
	var ws Workspace
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var can bool
		var id string
		var owned int
		// The user row lock makes concurrent creates count in turn.
		if err := tx.QueryRow(ctx, `select can_create_workspace(u.id), u.plan,
			        (select count(*) from workspaces where created_by = u.id)
			   from users u where u.id = $1 for no key update`, userID).Scan(&can, &id, &owned); err != nil {
			return fmt.Errorf("check workspace limit: %w", err)
		}
		p, _ := plan.Get(id)
		switch {
		case !can:
			return errNoCreate
		case p.Workspaces > 0 && owned >= p.Workspaces:
			return plan.Exceeded(fmt.Sprintf("Your plan allows %s. Upgrade to create more.", plan.Count(p.Workspaces, "workspace")))
		}
		var err error
		ws, err = create(ctx, tx, userID, name, slug)
		return err
	})
	return ws, err
}

// create runs in its own transaction on conn, or a savepoint when conn is a pgx.Tx (so errTaken leaves the caller's tx usable).
func create(ctx context.Context, conn interface {
	Begin(context.Context) (pgx.Tx, error)
}, userID, name, slug string) (Workspace, error) {
	ws := Workspace{Name: name, Slug: slug, Role: RoleRef{Name: "Admin"}, Permissions: allPermissions()}
	err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx,
			`insert into workspaces (name, slug, created_by) select $1, $2, $3 where can_create_workspace($3) returning id::text`,
			name, slug, userID).Scan(&ws.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			return errNoCreate
		}
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx,
			`with r as (insert into workspace_roles (workspace_id, name, builtin, permissions)
			   values ($1, 'Admin', 'admin', '{}'), ($1, 'Member', 'member', $2), ($1, 'Guest', 'guest', $2) returning id, builtin)
			 select id::text from r where builtin = 'admin'`,
			ws.ID, memberDefaults).Scan(&ws.Role.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`insert into workspace_members (workspace_id, user_id, role_id) values ($1, $2, $3)`, ws.ID, userID, ws.Role.ID); err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, realtime.AdminWorkspaces(userID))
	})
	if db.IsUniqueViolation(err) {
		return Workspace{}, errTaken
	}
	if err != nil {
		return Workspace{}, fmt.Errorf("create workspace: %w", err)
	}
	return ws, nil
}

// open marks the workspace as opened now by the user and returns it; nil when it's missing or the user isn't a member.
func (m *model) open(ctx context.Context, userID, slug string) (*Workspace, error) {
	ws, err := scanWorkspace(m.db.QueryRow(ctx,
		`update workspace_members m set last_opened_at = now()
		   from workspaces w, workspace_roles r
		  where w.id = m.workspace_id and r.id = m.role_id and w.slug = $1 and m.user_id = $2
		  returning `+wsCols, slug, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open workspace: %w", err)
	}
	return &ws, nil
}

// membership returns the workspace with the user's role and permissions; nil when it's missing or the user isn't a member.
func (m *model) membership(ctx context.Context, userID, slug string) (*Workspace, error) {
	ws, err := scanWorkspace(m.db.QueryRow(ctx,
		`select `+wsCols+` from `+wsFrom+` where w.slug = $1 and m.user_id = $2`, slug, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("workspace membership: %w", err)
	}
	return &ws, nil
}

// event is kind in the workspace ws, by the actor, with paths.
func event(ws *Workspace, actorID, kind string, paths ...string) realtime.Event {
	return realtime.Event{Kind: kind, Workspace: ws.Slug, Actor: actorID, Audience: realtime.ToWorkspace(ws.ID), Paths: paths}
}

// update renames the workspace and changes its slug, by the actor. Returns errTaken when the new slug belongs to
// another workspace.
func (m *model) update(ctx context.Context, ws *Workspace, actorID, name, slug string) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `update workspaces set name = $2, slug = $3 where id = $1`, ws.ID, name, slug); err != nil {
			return err
		}
		if name != ws.Name {
			if err := wsaudit.Record(ctx, tx, ws.ID, actorID, "workspace.renamed", name, map[string]any{"from": ws.Name, "to": name}); err != nil {
				return err
			}
		}
		if slug != ws.Slug {
			if err := wsaudit.Record(ctx, tx, ws.ID, actorID, "workspace.url_changed", slug, map[string]any{"from": ws.Slug, "to": slug}); err != nil {
				return err
			}
		}
		e := event(ws, actorID, "workspace.changed", realtime.Workspace(ws.Slug), "/workspaces")
		if slug != ws.Slug {
			e.NewSlug = slug
		}
		if err := realtime.Publish(ctx, tx, e); err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, realtime.AdminWorkspaces(actorID))
	})
	if db.IsUniqueViolation(err) {
		return errTaken
	}
	if err != nil {
		return fmt.Errorf("update workspace: %w", err)
	}
	return nil
}

// delete removes the workspace, by the actor; members, roles and invites go with it (on delete cascade).
func (m *model) delete(ctx context.Context, ws *Workspace, actorID string) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `delete from workspaces where id = $1`, ws.ID); err != nil {
			return err
		}
		e := event(ws, actorID, "workspace.removed", "/workspaces")
		e.WorkspaceID = ws.ID
		if err := realtime.Publish(ctx, tx, e); err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, realtime.AdminWorkspaces(actorID))
	})
	if err != nil {
		return fmt.Errorf("delete workspace: %w", err)
	}
	return nil
}

// --- members and invites ---

var errWrongEmail = errors.New("invite is for another email")

type Member struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Email string  `json:"email"`
	Role  RoleRef `json:"role"`
	Guest bool    `json:"guest"`
}

// Invite is a pending invite; InvitedBy is the inviter's name, "" once they're deleted.
type Invite struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	InvitedBy string    `json:"invited_by"`
	ExpiresAt time.Time `json:"expires_at"`
	Role      RoleRef   `json:"role"`
}

// InviteInfo is what the public invite page shows.
type InviteInfo struct {
	Workspace string `json:"workspace"`
	Email     string `json:"email"`
	InvitedBy string `json:"invited_by"`
}

// memberCols selects a Member from workspace_members m, users u and workspace_roles r.
const memberCols = `u.id::text, u.name, u.email, r.id::text, r.name, r.builtin is not distinct from 'guest'`

// memberSeen limits members m (users u, roles r) of workspace $1 to those viewer $2 may see: everyone, but a guest sees
// only admins, themselves and who can see a project they're in.
const memberSeen = ` and (not is_guest($1, $2) or r.builtin = 'admin' or u.id = $2
	or exists (select 1 from project_members pm join projects p on p.id = pm.project_id
	           where pm.user_id = $2 and p.workspace_id = $1 and can_see_project(p.id, u.id)))`

func scanMember(row pgx.Row) (Member, error) {
	var mb Member
	err := row.Scan(&mb.ID, &mb.Name, &mb.Email, &mb.Role.ID, &mb.Role.Name, &mb.Guest)
	return mb, err
}

// members returns the workspace's members the viewer may see (memberSeen), admins first, then by name. Not paginated: every member is needed at
// once by the assignee and mention pickers and the people filters, and a workspace's members are bounded by its
// team size (hundreds), unlike an issue's timeline.
func (m *model) members(ctx context.Context, workspaceID, viewerID string) ([]Member, error) {
	rows, err := m.db.Query(ctx,
		`select `+memberCols+`
		   from workspace_members m join users u on u.id = m.user_id join workspace_roles r on r.id = m.role_id
		  where m.workspace_id = $1`+memberSeen+`
		  order by r.builtin is not distinct from 'admin' desc, lower(u.name), u.email`, workspaceID, viewerID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	ms, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Member, error) { return scanMember(row) })
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	return ms, nil
}

// memberPage returns limit of the workspace's members the viewer may see from offset, in members' order, whose name or email contains q
// (any case; "" matches everyone), and how many match in all (0 past the end).
// ponytail: offset paging, so a member joining or leaving mid-scroll shifts the pages by one; keyset on
// (admin, lower(name), email) if that shows up.
func (m *model) memberPage(ctx context.Context, workspaceID, viewerID, q string, offset, limit int) ([]Member, int, error) {
	rows, err := m.db.Query(ctx,
		`select `+memberCols+`, count(*) over ()
		   from workspace_members m join users u on u.id = m.user_id join workspace_roles r on r.id = m.role_id
		  where m.workspace_id = $1 and (strpos(lower(u.name), lower($3)) > 0 or strpos(lower(u.email), lower($3)) > 0)`+memberSeen+`
		  order by r.builtin is not distinct from 'admin' desc, lower(u.name), u.email
		  offset $4 limit $5`, workspaceID, viewerID, q, offset, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list members: %w", err)
	}
	total := 0
	ms, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Member, error) {
		var mb Member
		err := row.Scan(&mb.ID, &mb.Name, &mb.Email, &mb.Role.ID, &mb.Role.Name, &mb.Guest, &total)
		return mb, err
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list members: %w", err)
	}
	return ms, total, nil
}

// inviteCols selects an Invite from a CTE or table aliased i; a null role_id is the Member role.
const inviteCols = `select i.id::text, i.email, coalesce(u.name, ''), i.expires_at, r.id::text, r.name
	from i left join users u on u.id = i.invited_by
	join workspace_roles r on r.workspace_id = i.workspace_id and (r.id = i.role_id or i.role_id is null and r.builtin = 'member')`

func scanInvite(row pgx.Row) (Invite, error) {
	var inv Invite
	err := row.Scan(&inv.ID, &inv.Email, &inv.InvitedBy, &inv.ExpiresAt, &inv.Role.ID, &inv.Role.Name)
	inv.ExpiresAt = inv.ExpiresAt.UTC()
	return inv, err
}

// invites returns the workspace's invites, expired included, newest first. An invite whose person already joined
// another way isn't pending any more: its link still lands them here, but it's hidden.
func (m *model) invites(ctx context.Context, workspaceID string) ([]Invite, error) {
	rows, err := m.db.Query(ctx,
		`with i as (select * from workspace_invites v where workspace_id = $1 and not exists (select 1 from workspace_members m
		   join users u on u.id = m.user_id where m.workspace_id = v.workspace_id and u.email = v.email))
		 `+inviteCols+` order by i.created_at desc`,
		workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list invites: %w", err)
	}
	invs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Invite, error) { return scanInvite(row) })
	if err != nil {
		return nil, fmt.Errorf("list invites: %w", err)
	}
	return invs, nil
}

// isMember reports whether an account with this email is in the workspace.
// verified reports whether the user's email is verified.
func (m *model) verified(ctx context.Context, userID string) (bool, error) {
	var ok bool
	err := m.db.QueryRow(ctx, `select email_verified_at is not null from users where id = $1`, userID).Scan(&ok)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("read verification: %w", err)
	}
	return ok, nil
}

// takeInviteSend counts one invite email by the user in the workspace, unless the user has sent perUser or the
// workspace perWorkspace in the last day; then it returns false. Rows older than a day are dropped.
// ponytail: two concurrent sends can both pass the last free slot; take a per-workspace advisory lock if that matters.
func (m *model) takeInviteSend(ctx context.Context, workspaceID, userID string, perUser, perWorkspace int) (bool, error) {
	tag, err := m.db.Exec(ctx, `
		with gone as (delete from invite_sends where at < now() - interval '1 day')
		insert into invite_sends (workspace_id, user_id)
		select $1, $2
		where (select count(*) from invite_sends where user_id = $2 and at > now() - interval '1 day') < $3
		  and (select count(*) from invite_sends where workspace_id = $1 and at > now() - interval '1 day') < $4`,
		workspaceID, userID, perUser, perWorkspace)
	if err != nil {
		return false, fmt.Errorf("count invite send: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (m *model) isMember(ctx context.Context, workspaceID, email string) (bool, error) {
	var ok bool
	err := m.db.QueryRow(ctx,
		`select exists(select 1 from workspace_members m join users u on u.id = m.user_id
		  where m.workspace_id = $1 and u.email = $2)`, workspaceID, email).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("check member: %w", err)
	}
	return ok, nil
}

// seatsFull reports whether the workspace's members plus its pending invites to other emails than email fill its
// plan, and the plan's member limit.
// ponytail: concurrent invites each see the same count and can overshoot by a few; joining still stops at the limit.
func (m *model) seatsFull(ctx context.Context, workspaceID, email string) (bool, int, error) {
	p, err := plan.ForWorkspace(ctx, m.db, workspaceID)
	if err != nil || p.Members == 0 {
		return false, 0, err
	}
	var n int
	if err := m.db.QueryRow(ctx,
		`select (select count(*) from workspace_members where workspace_id = $1)
		      + (select count(*) from workspace_invites where workspace_id = $1 and email <> $2 and expires_at > now())`,
		workspaceID, email).Scan(&n); err != nil {
		return false, 0, fmt.Errorf("count seats: %w", err)
	}
	return n >= p.Members, p.Members, nil
}

// roomFor locks the workspace row in tx (so concurrent joins count in turn) and returns errFull when its members
// fill its owner's plan and the user isn't one of them.
func roomFor(ctx context.Context, tx pgx.Tx, workspaceID, userID string) error {
	var id string
	var n int
	if err := tx.QueryRow(ctx, `select coalesce(u.plan, 'starter'),
		        (select count(*) from workspace_members m where m.workspace_id = w.id and m.user_id <> $2)
		   from workspaces w left join users u on u.id = w.created_by where w.id = $1 for no key update of w`,
		workspaceID, userID).Scan(&id, &n); err != nil {
		return fmt.Errorf("check room: %w", err)
	}
	if p, ok := plan.Get(id); ok && p.Members > 0 && n >= p.Members {
		return errFull
	}
	return nil
}

// invitesChanged is the event for a change to the workspace's invites (the members page lists them).
func invitesChanged(ws *Workspace, actorID string) realtime.Event {
	p := realtime.Workspace(ws.Slug)
	return event(ws, actorID, "members.changed", p+"/invites", p+"/members")
}

// projectIDs returns the ids of the workspace's projects with the keys (or their old keys) the viewer can see; nil
// unless every key names one.
func (m *model) projectIDs(ctx context.Context, workspaceID, viewerID string, keys []string) ([]string, error) {
	if len(keys) == 0 || len(keys) > 100 {
		return nil, nil
	}
	var ids []string
	var all bool
	err := m.db.QueryRow(ctx, `
		with k as (select distinct canonical_key($1, upper(trim(x))) key from unnest($3::text[]) x),
		     p as (select p.id from projects p join k on k.key = p.key where p.workspace_id = $1 and can_see_project(p.id, $2))
		select coalesce(array(select id::text from p), '{}'), (select count(*) from p) = (select count(*) from k)`,
		workspaceID, viewerID, keys).Scan(&ids, &all)
	if err != nil {
		return nil, fmt.Errorf("find projects: %w", err)
	}
	if !all {
		return nil, nil
	}
	return ids, nil
}

// joinGuestProjects adds the user, when a guest of the invite's workspace, to the projects the invites with the ids
// name (still existing ones).
func joinGuestProjects(ctx context.Context, tx pgx.Tx, userID string, inviteIDs []string) error {
	_, err := tx.Exec(ctx, `
		insert into project_members (project_id, user_id, added_by)
		select p.id, $1, i.invited_by
		  from workspace_invites i
		  join workspace_members m on m.workspace_id = i.workspace_id and m.user_id = $1
		  join workspace_roles r on r.id = m.role_id and r.builtin = 'guest'
		  join projects p on p.workspace_id = i.workspace_id and p.id = any(i.project_ids)
		 where i.id = any($2::uuid[])
		on conflict do nothing`, userID, inviteIDs)
	if err != nil {
		return fmt.Errorf("join guest projects: %w", err)
	}
	return nil
}

// invite creates the invite, or replaces the token, expiry, inviter, role and projects (a guest's) of a pending one
// for the same email; that one's token keeps working too, so the link already sent isn't broken.
func (m *model) invite(ctx context.Context, ws *Workspace, email, invitedBy, roleID string, projectIDs []string, tokenHash []byte, days int) (Invite, error) {
	var inv Invite
	if projectIDs == nil {
		projectIDs = []string{}
	}
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var err error
		if inv, err = scanInvite(tx.QueryRow(ctx,
			`with i as (
			   insert into workspace_invites (workspace_id, email, invited_by, role_id, token_hash, expires_at, project_ids)
			   values ($1, $2, $3, $4, $5, now() + make_interval(days => $6), $7)
			   on conflict (workspace_id, email) do update
			     set invited_by = excluded.invited_by, role_id = excluded.role_id,
			         prev_token_hash = case when workspace_invites.expires_at > now() then workspace_invites.token_hash end,
			         token_hash = excluded.token_hash, expires_at = excluded.expires_at, project_ids = excluded.project_ids
			   returning *) `+inviteCols,
			ws.ID, email, invitedBy, roleID, tokenHash, days, projectIDs)); err != nil {
			return err
		}
		if err := inviteTouched(ctx, tx, inv.ID, invitedBy); err != nil {
			return err
		}
		var guest bool
		if err := tx.QueryRow(ctx, `select builtin is not distinct from 'guest' from workspace_roles where id = $1`, roleID).Scan(&guest); err != nil {
			return err
		}
		if err := wsaudit.Record(ctx, tx, ws.ID, invitedBy, "member.invited", inv.Email,
			map[string]any{"role": inv.Role.Name, "guest": guest}); err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, invitesChanged(ws, invitedBy))
	})
	if err != nil {
		return Invite{}, fmt.Errorf("create invite: %w", err)
	}
	return inv, nil
}

// renew gives the workspace's invite a new token and expiry, by the actor; nil when there's no such invite.
func (m *model) renew(ctx context.Context, ws *Workspace, actorID, id string, tokenHash []byte, days int) (*Invite, error) {
	if !web.IsUUID(id) {
		return nil, nil
	}
	var inv Invite
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var err error
		if inv, err = scanInvite(tx.QueryRow(ctx,
			`with i as (
			   update workspace_invites set token_hash = $3, prev_token_hash = null, expires_at = now() + make_interval(days => $4)
			    where workspace_id = $1 and id = $2
			   returning *) `+inviteCols,
			ws.ID, id, tokenHash, days)); err != nil {
			return err
		}
		if err := inviteTouched(ctx, tx, inv.ID, actorID); err != nil {
			return err
		}
		if err := wsaudit.Record(ctx, tx, ws.ID, actorID, "invite.resent", inv.Email, nil); err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, invitesChanged(ws, actorID))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("renew invite: %w", err)
	}
	return &inv, nil
}

// cancelInvite deletes the workspace's invite, by the actor; false when there's no such invite.
func (m *model) cancelInvite(ctx context.Context, ws *Workspace, actorID, id string) (bool, error) {
	if !web.IsUUID(id) {
		return false, nil
	}
	deleted := false
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if err := inviteTouched(ctx, tx, id, actorID); err != nil {
			return err
		}
		var email string
		err := tx.QueryRow(ctx, `delete from workspace_invites where workspace_id = $1 and id = $2 returning email`, ws.ID, id).Scan(&email)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		deleted = true
		if err := wsaudit.Record(ctx, tx, ws.ID, actorID, "invite.canceled", email, nil); err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, invitesChanged(ws, actorID))
	})
	if err != nil {
		return false, fmt.Errorf("cancel invite: %w", err)
	}
	return deleted, nil
}

// inviteInfo returns the unexpired invite with this token hash; nil when there's none.
func (m *model) inviteInfo(ctx context.Context, tokenHash []byte) (*InviteInfo, error) {
	var info InviteInfo
	err := m.db.QueryRow(ctx,
		`select w.name, i.email, coalesce(u.name, '')
		   from workspace_invites i join workspaces w on w.id = i.workspace_id left join users u on u.id = i.invited_by
		  where $1 in (i.token_hash, i.prev_token_hash) and i.expires_at > now()`, tokenHash).Scan(&info.Workspace, &info.Email, &info.InvitedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read invite: %w", err)
	}
	return &info, nil
}

// accept joins the user to the workspace of the invite with this token (see join).
// Returns nil when the token is unknown or expired, and errWrongEmail plus the invite's email when the user's differs.
func (m *model) accept(ctx context.Context, userID string, tokenHash []byte) (*Workspace, string, error) {
	var ws *Workspace
	var inviteEmail string
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var id, workspaceID, userEmail string
		err := tx.QueryRow(ctx,
			`select i.id::text, i.workspace_id::text, i.email, u.email
			   from workspace_invites i, users u
			  where $1 in (i.token_hash, i.prev_token_hash) and i.expires_at > now() and u.id = $2
			    for update of i`, tokenHash, userID).Scan(&id, &workspaceID, &inviteEmail, &userEmail)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if userEmail != inviteEmail {
			return errWrongEmail
		}
		ws, err = join(ctx, tx, userID, id, workspaceID)
		return err
	})
	if errors.Is(err, errWrongEmail) {
		return nil, inviteEmail, err
	}
	if err != nil {
		return nil, "", fmt.Errorf("accept invite: %w", err)
	}
	return ws, inviteEmail, nil
}

// join adds the user to the invite's workspace with the invite's role (keeping any existing role), and a guest to its
// projects (errFull when the plan's members are taken), and deletes the invite with its invite notifications, in tx. Returns the workspace as the user sees it.
func join(ctx context.Context, tx pgx.Tx, userID, inviteID, workspaceID string) (*Workspace, error) {
	if err := roomFor(ctx, tx, workspaceID, userID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx,
		`insert into workspace_members (workspace_id, user_id, role_id)
		 select i.workspace_id, $2, coalesce(i.role_id, r.id)
		   from workspace_invites i join workspace_roles r on r.workspace_id = i.workspace_id and r.builtin = 'member'
		  where i.id = $1
		 on conflict do nothing`, inviteID, userID); err != nil {
		return nil, err
	}
	if err := joinGuestProjects(ctx, tx, userID, []string{inviteID}); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `delete from notifications n using workspaces w
		where n.invite_id = $1 and w.id = n.workspace_id returning n.user_id::text, w.slug`, inviteID)
	if err != nil {
		return nil, err
	}
	type note struct{ user, slug string }
	notes, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (note, error) {
		var n note
		return n, r.Scan(&n.user, &n.slug)
	})
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `delete from workspace_invites where id = $1`, inviteID); err != nil {
		return nil, err
	}
	ws, err := scanWorkspace(tx.QueryRow(ctx,
		`select `+wsCols+` from `+wsFrom+` where w.id = $1 and m.user_id = $2`, workspaceID, userID))
	if err != nil {
		return nil, err
	}
	var email string
	if err := tx.QueryRow(ctx, `select email from users where id = $1`, userID).Scan(&email); err != nil {
		return nil, err
	}
	if err := wsaudit.Record(ctx, tx, workspaceID, userID, "member.joined", email, map[string]any{"role": ws.Role.Name}); err != nil {
		return nil, err
	}
	events := []realtime.Event{invitesChanged(&ws, userID), realtime.AdminWorkspaces(userID), realtime.InvitesChanged(userID, userID)}
	for _, n := range notes {
		events = append(events, realtime.NotificationsChanged(n.slug, userID, n.user))
	}
	for _, e := range events {
		if err := realtime.Publish(ctx, tx, e); err != nil {
			return nil, err
		}
	}
	return &ws, nil
}

// inviteTouched tells the invited person's tabs, when they have an account, that their pending invites changed, and
// the tabs of whoever holds an invite notification for it that its availability may have. Call it in the
// transaction that changes the invite, before a delete.
func inviteTouched(ctx context.Context, tx pgx.Tx, inviteID, actorID string) error {
	rows, err := tx.Query(ctx,
		`select u.id::text, '' from workspace_invites i join users u on u.email = i.email where i.id = $1
		 union all
		 select n.user_id::text, w.slug from notifications n join workspaces w on w.id = n.workspace_id where n.invite_id = $1`,
		inviteID)
	if err != nil {
		return fmt.Errorf("invite audience: %w", err)
	}
	type to struct{ user, slug string }
	tos, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (to, error) {
		var t to
		return t, r.Scan(&t.user, &t.slug)
	})
	if err != nil {
		return fmt.Errorf("invite audience: %w", err)
	}
	for _, t := range tos {
		e := realtime.InvitesChanged(actorID, t.user)
		if t.slug != "" {
			e = realtime.NotificationsChanged(t.slug, actorID, t.user)
		}
		if err := realtime.Publish(ctx, tx, e); err != nil {
			return err
		}
	}
	return nil
}

// PendingInvite is an invite to another workspace for the signed-in user's email, as the invite prompt shows it.
type PendingInvite struct {
	ID        string `json:"id"`
	Workspace struct {
		Name string `json:"name"`
	} `json:"workspace"`
	InvitedBy string    `json:"invited_by"` // "" once the inviter is deleted
	Role      RoleRef   `json:"role"`
	Guest     bool      `json:"guest"`
	ExpiresAt time.Time `json:"expires_at"`
	Dismissed bool      `json:"dismissed"` // "Not now": listed in the switcher, not prompted again
}

// mine joins invites i to the user $1 they're for, whose email must be verified (whoever owns the inbox owns the invite).
const mine = ` join users me on me.email = i.email and me.id = $1 and me.email_verified_at is not null`

// myInvites returns the user's unexpired invites to workspaces they aren't in, oldest first; dismissed ones are flagged.
func (m *model) myInvites(ctx context.Context, userID string) ([]PendingInvite, error) {
	rows, err := m.db.Query(ctx,
		`select i.id::text, w.name, coalesce(u.name, ''), r.id::text, r.name, r.builtin is not distinct from 'guest', i.expires_at,
		        i.dismissed_at is not null
		   from workspace_invites i`+mine+`
		   join workspaces w on w.id = i.workspace_id
		   left join users u on u.id = i.invited_by
		   join workspace_roles r on r.workspace_id = i.workspace_id and (r.id = i.role_id or i.role_id is null and r.builtin = 'member')
		  where i.expires_at > now()
		    and not exists (select 1 from workspace_members m where m.workspace_id = i.workspace_id and m.user_id = $1)
		  order by i.created_at, i.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("list my invites: %w", err)
	}
	invs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (PendingInvite, error) {
		var p PendingInvite
		err := row.Scan(&p.ID, &p.Workspace.Name, &p.InvitedBy, &p.Role.ID, &p.Role.Name, &p.Guest, &p.ExpiresAt, &p.Dismissed)
		p.ExpiresAt = p.ExpiresAt.UTC()
		return p, err
	})
	if err != nil {
		return nil, fmt.Errorf("list my invites: %w", err)
	}
	return invs, nil
}

// acceptByID joins the user to the workspace of their unexpired invite with the id (see join); nil when there's no
// such invite for their email.
func (m *model) acceptByID(ctx context.Context, userID, id string) (*Workspace, error) {
	if !web.IsUUID(id) {
		return nil, nil
	}
	var ws *Workspace
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var workspaceID string
		err := tx.QueryRow(ctx, `select i.workspace_id::text from workspace_invites i`+mine+`
			where i.id = $2 and i.expires_at > now() for update of i`, userID, id).Scan(&workspaceID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		ws, err = join(ctx, tx, userID, id, workspaceID)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("accept invite: %w", err)
	}
	return ws, nil
}

// dismiss records the user's "Not now" on their unexpired invite with the id: the first time, it sets dismissed_at
// and leaves an invite notification from the inviter in ws, the workspace the user is in. False when there's no
// such invite for their email.
func (m *model) dismiss(ctx context.Context, userID, id string, ws *Workspace) (bool, error) {
	if !web.IsUUID(id) {
		return false, nil
	}
	found := false
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var dismissed bool
		err := tx.QueryRow(ctx, `select i.dismissed_at is not null from workspace_invites i`+mine+`
			where i.id = $2 and i.expires_at > now() for update of i`, userID, id).Scan(&dismissed)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if found = true; dismissed {
			return nil
		}
		if _, err := tx.Exec(ctx, `update workspace_invites set dismissed_at = now() where id = $1`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`insert into notifications (user_id, workspace_id, invite_id, actor_id, kind, data)
			 select $1, $2, i.id, i.invited_by, 'invite', jsonb_build_object('invite_id', i.id, 'workspace', w.name)
			   from workspace_invites i join workspaces w on w.id = i.workspace_id where i.id = $3`, userID, ws.ID, id); err != nil {
			return err
		}
		if err := realtime.Publish(ctx, tx, realtime.NotificationsChanged(ws.Slug, userID, userID)); err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, realtime.InvitesChanged(userID, userID))
	})
	if err != nil {
		return false, fmt.Errorf("dismiss invite: %w", err)
	}
	return found, nil
}

// --- roles ---

// Role is a workspace role; Builtin is nil for custom roles, and Admin's Permissions are the whole catalog.
type Role struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Builtin     *string  `json:"builtin"`
	Permissions []string `json:"permissions"`
	MemberCount int      `json:"member_count"`
}

// roleCols selects a Role from workspace_roles r.
const roleCols = `r.id::text, r.name, r.builtin, r.permissions,
	(select count(*) from workspace_members m where m.role_id = r.id)`

func scanRole(row pgx.Row) (Role, error) {
	var r Role
	err := row.Scan(&r.ID, &r.Name, &r.Builtin, &r.Permissions, &r.MemberCount)
	if r.Builtin != nil {
		r.Permissions = Effective(*r.Builtin, r.Permissions)
	}
	return r, err
}

// roles returns the workspace's roles: Admin, Member, Guest, then custom ones by name.
func (m *model) roles(ctx context.Context, workspaceID string) ([]Role, error) {
	rows, err := m.db.Query(ctx,
		`select `+roleCols+` from workspace_roles r where r.workspace_id = $1
		  order by case r.builtin when 'admin' then 0 when 'member' then 1 when 'guest' then 2 else 3 end, lower(r.name)`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	rs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Role, error) { return scanRole(row) })
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	return rs, nil
}

// role returns the workspace's role with this id, or its Member role when id is ""; nil when there's no such role.
func (m *model) role(ctx context.Context, workspaceID, id string) (*Role, error) {
	var roleID any // nil: the Member role
	if id != "" {
		if !web.IsUUID(id) {
			return nil, nil
		}
		roleID = id
	}
	r, err := scanRole(m.db.QueryRow(ctx,
		`select `+roleCols+` from workspace_roles r
		  where r.workspace_id = $1 and (r.id = $2 or $2::uuid is null and r.builtin = 'member')`, workspaceID, roleID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read role: %w", err)
	}
	return &r, nil
}

// memberRole returns the member's role; nil when they aren't a member.
func (m *model) memberRole(ctx context.Context, workspaceID, userID string) (*Role, error) {
	if !web.IsUUID(userID) {
		return nil, nil
	}
	r, err := scanRole(m.db.QueryRow(ctx,
		`select `+roleCols+` from workspace_roles r join workspace_members m on m.role_id = r.id
		  where m.workspace_id = $1 and m.user_id = $2`, workspaceID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read member role: %w", err)
	}
	return &r, nil
}

// rolesChanged publishes roles.changed, and workspace.permissions to every member holding the role with the id
// (none when it's "").
func rolesChanged(ctx context.Context, tx pgx.Tx, ws *Workspace, actorID, roleID string) error {
	p := realtime.Workspace(ws.Slug)
	if err := realtime.Publish(ctx, tx, event(ws, actorID, "roles.changed", p+"/roles", p+"/members")); err != nil {
		return err
	}
	if roleID == "" {
		return nil
	}
	rows, err := tx.Query(ctx, `select user_id::text from workspace_members where workspace_id = $1 and role_id = $2`, ws.ID, roleID)
	if err != nil {
		return err
	}
	holders, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	// ponytail: one notification per holder; a role held by thousands would want one workspace-wide event instead.
	for _, u := range holders {
		if err := realtime.Publish(ctx, tx, permissionsChanged(ws, actorID, u)); err != nil {
			return err
		}
	}
	return nil
}

// permissionsChanged tells the member's tabs their permissions in the workspace changed. Their sockets reconnect, so
// live editing re-joins with the new permissions.
func permissionsChanged(ws *Workspace, actorID, userID string) realtime.Event {
	e := event(ws, actorID, "workspace.permissions", realtime.Workspace(ws.Slug), realtime.All(ws.Slug))
	e.Audience = realtime.ToUser(userID)
	e.Close = true
	return e
}

// createRole adds a custom role, by the actor. Returns errNameTaken when the workspace has a role with that name
// (any case).
func (m *model) createRole(ctx context.Context, ws *Workspace, actorID, name string, perms []string) (Role, error) {
	var r Role
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var err error
		if r, err = scanRole(tx.QueryRow(ctx,
			`with r as (insert into workspace_roles (workspace_id, name, permissions) values ($1, $2, $3) returning *)
			 select `+roleCols+` from r`, ws.ID, name, perms)); err != nil {
			return err
		}
		if err := wsaudit.Record(ctx, tx, ws.ID, actorID, "role.created", name, map[string]any{"permissions": perms}); err != nil {
			return err
		}
		return rolesChanged(ctx, tx, ws, actorID, "")
	})
	if db.IsUniqueViolation(err) {
		return Role{}, errNameTaken
	}
	if err != nil {
		return Role{}, fmt.Errorf("create role: %w", err)
	}
	return r, nil
}

// updateRole renames the role and replaces its permissions, by the actor; nil when there's no such role.
// Returns errNameTaken like createRole.
func (m *model) updateRole(ctx context.Context, ws *Workspace, actorID, id, name string, perms []string) (*Role, error) {
	if !web.IsUUID(id) {
		return nil, nil
	}
	var r Role
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var oldName string
		var oldPerms []string
		if err := tx.QueryRow(ctx, `select name, permissions from workspace_roles where workspace_id = $1 and id = $2 for update`,
			ws.ID, id).Scan(&oldName, &oldPerms); err != nil {
			return err
		}
		var err error
		if r, err = scanRole(tx.QueryRow(ctx,
			`with r as (update workspace_roles set name = $3, permissions = $4
			             where workspace_id = $1 and id = $2 returning *)
			 select `+roleCols+` from r`, ws.ID, id, name, perms)); err != nil {
			return err
		}
		detail := map[string]any{"added": without(perms, oldPerms), "removed": without(oldPerms, perms)}
		if oldName != name {
			detail["renamed_from"] = oldName
		}
		if err := wsaudit.Record(ctx, tx, ws.ID, actorID, "role.updated", name, detail); err != nil {
			return err
		}
		return rolesChanged(ctx, tx, ws, actorID, r.ID)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if db.IsUniqueViolation(err) {
		return nil, errNameTaken
	}
	if err != nil {
		return nil, fmt.Errorf("update role: %w", err)
	}
	return &r, nil
}

// deleteRole deletes a custom role, by the actor, moving its members and invites to the Member role; false when
// there's no such role.
func (m *model) deleteRole(ctx context.Context, ws *Workspace, actorID, id string) (bool, error) {
	if !web.IsUUID(id) {
		return false, nil
	}
	workspaceID := ws.ID
	deleted := false
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		// Its holders' permissions change: tell them before they move to Member.
		if err := rolesChanged(ctx, tx, ws, actorID, id); err != nil {
			return err
		}
		var memberRole string
		if err := tx.QueryRow(ctx, `select id::text from workspace_roles where workspace_id = $1 and builtin = 'member'`,
			workspaceID).Scan(&memberRole); err != nil {
			return err
		}
		for _, q := range []string{
			`update workspace_members set role_id = $3 where workspace_id = $1 and role_id = $2`,
			`update workspace_invites set role_id = $3 where workspace_id = $1 and role_id = $2`,
		} {
			if _, err := tx.Exec(ctx, q, workspaceID, id, memberRole); err != nil {
				return err
			}
		}
		var name string
		err := tx.QueryRow(ctx,
			`delete from workspace_roles where workspace_id = $1 and id = $2 and builtin is null returning name`, workspaceID, id).Scan(&name)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		deleted = true
		return wsaudit.Record(ctx, tx, workspaceID, actorID, "role.deleted", name, nil)
	})
	if err != nil {
		return false, fmt.Errorf("delete role: %w", err)
	}
	return deleted, nil
}

// setMemberRole changes a member's role, by the actor; nil when they aren't a member. Returns errLastAdmin when it
// would leave no admin.
func (m *model) setMemberRole(ctx context.Context, ws *Workspace, actorID, userID, roleID string) (*Member, error) {
	if !web.IsUUID(userID) {
		return nil, nil
	}
	var mb *Member
	err := m.keepingAdmin(ctx, ws.ID, func(tx pgx.Tx) error {
		var from string
		err := tx.QueryRow(ctx, `select r.name from workspace_members m join workspace_roles r on r.id = m.role_id
			where m.workspace_id = $1 and m.user_id = $2`, ws.ID, userID).Scan(&from)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		r, err := scanMember(tx.QueryRow(ctx,
			`with m as (update workspace_members set role_id = $3 where workspace_id = $1 and user_id = $2 returning *)
			 select `+memberCols+` from m join users u on u.id = m.user_id join workspace_roles r on r.id = m.role_id`,
			ws.ID, userID, roleID))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		mb = &r
		if from != r.Role.Name {
			if err := wsaudit.Record(ctx, tx, ws.ID, actorID, "member.role_changed", r.Email, map[string]any{"from": from, "to": r.Role.Name}); err != nil {
				return err
			}
		}
		if err := realtime.Publish(ctx, tx, event(ws, actorID, "members.changed", realtime.Workspace(ws.Slug)+"/members")); err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, permissionsChanged(ws, actorID, mb.ID))
	})
	if errors.Is(err, errLastAdmin) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("set member role: %w", err)
	}
	return mb, nil
}

// removeMember takes the user out of the workspace and its private projects, deletes their saved views there and
// unassigns their issues there, by the actor (the user when they leave); false when they aren't a member. Returns errLastAdmin when it would leave no admin.
func (m *model) removeMember(ctx context.Context, ws *Workspace, actorID, userID string) (bool, error) {
	if !web.IsUUID(userID) {
		return false, nil
	}
	removed := false
	err := m.keepingAdmin(ctx, ws.ID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `delete from workspace_members where workspace_id = $1 and user_id = $2`, ws.ID, userID)
		if removed = tag.RowsAffected() == 1; err != nil || !removed {
			return err
		}
		action := "member.removed"
		if actorID == userID {
			action = "member.left"
		}
		var email string
		if err := tx.QueryRow(ctx, `select email from users where id = $1`, userID).Scan(&email); err != nil {
			return err
		}
		if err := wsaudit.Record(ctx, tx, ws.ID, actorID, action, email, nil); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `update issues_all set assignee_id = null where workspace_id = $1 and assignee_id = $2`, ws.ID, userID); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `delete from project_members where user_id = $2
			and project_id in (select id from projects where workspace_id = $1)`, ws.ID, userID); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `delete from saved_views where workspace_id = $1 and user_id = $2`, ws.ID, userID); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `delete from note_members where user_id = $2 and note_id in (select id from notes where workspace_id = $1)`,
			ws.ID, userID); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `delete from notes where workspace_id = $1 and owner_id = $2`, ws.ID, userID); err != nil {
			return err
		}
		// Notes: their shared notes leave everyone's page, and they leave others'.
		if err := realtime.Publish(ctx, tx, event(ws, actorID, "members.changed", realtime.Workspace(ws.Slug)+"/members",
			realtime.Notes(ws.Slug))); err != nil {
			return err
		}
		gone := event(ws, actorID, "workspace.removed", "/workspaces")
		gone.Audience, gone.WorkspaceID = realtime.ToUser(userID), ws.ID
		if err := realtime.Publish(ctx, tx, gone); err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, realtime.AdminWorkspaces(actorID))
	})
	if errors.Is(err, errLastAdmin) {
		return false, err
	}
	if err != nil {
		return false, fmt.Errorf("remove member: %w", err)
	}
	return removed, nil
}

// keepingAdmin runs fn in a transaction and rolls it back with errLastAdmin if the workspace is left without an admin.
// Locking the Admin role row serializes concurrent changes, so two admins can't demote each other at once.
func (m *model) keepingAdmin(ctx context.Context, workspaceID string, fn func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var adminRole string
		if err := tx.QueryRow(ctx, `select id::text from workspace_roles where workspace_id = $1 and builtin = 'admin' for update`,
			workspaceID).Scan(&adminRole); err != nil {
			return err
		}
		if err := fn(tx); err != nil {
			return err
		}
		var ok bool
		if err := tx.QueryRow(ctx, `select exists(select 1 from workspace_members where role_id = $1)`, adminRole).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return errLastAdmin
		}
		return nil
	})
}
