// Package wsaudit is the workspace audit log: who changed the workspace itself (members, roles, invites, projects,
// integrations, webhooks, permanent deletes). Record writes an entry in the caller's transaction; the workspace
// package serves the list (docs/workspace-audit-plan.md).
package wsaudit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/web"
)

// Execer is a transaction (or a pool, outside one).
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Record adds an entry by the actor ("" for nobody), copying their name and email, with the client IP of the request
// on ctx (web.IP). Call it in the transaction that makes the change, so the two commit together. A project.* action's
// target is the project's key and an issue.* action's the issue's identifier (KEY-number): the entry keeps that
// project's id, so List shows it only to who can see the project.
func Record(ctx context.Context, q Execer, workspaceID, actorID, action, target string, detail map[string]any) error {
	if _, err := q.Exec(ctx,
		`insert into workspace_audit (workspace_id, actor_id, actor_name, actor_email, action, target, detail, ip, project_id)
		 select $1, u.id, coalesce(u.name, ''), coalesce(u.email, ''), $3, $4, coalesce($5::jsonb, '{}'), $6,
		        (select p.id from projects p where p.workspace_id = $1 and p.key = case
		           when $3 like 'project.%' then $4 when $3 like 'issue.%' then regexp_replace($4, '-[0-9]+$', '') end)
		   from (select) s left join users u on u.id = nullif($2, '')::uuid`,
		workspaceID, actorID, action, target, detail, web.IP(ctx)); err != nil {
		return fmt.Errorf("record %s: %w", action, err)
	}
	return nil
}

// Categories maps the list's category filter to the action prefixes (before the dot) it shows.
var Categories = map[string][]string{
	"members":      {"member", "invite"},
	"roles":        {"role"},
	"projects":     {"project"},
	"workspace":    {"workspace"},
	"integrations": {"webhook", "integration"},
	"trash":        {"trash", "issue"},
}

// Actor is who made the change; ID is nil once they're deleted (Name and Email are the copies).
type Actor struct {
	ID    *string `json:"id"`
	Name  string  `json:"name"`
	Email string  `json:"email"`
}

type Entry struct {
	ID     string          `json:"id"`
	At     time.Time       `json:"at"`
	Actor  *Actor          `json:"actor"` // nil: nobody, or a deleted user with no copies
	Action string          `json:"action"`
	Target string          `json:"target"`
	Detail json.RawMessage `json:"detail"`
	IP     string          `json:"ip"`
}

// PageSize is how many entries List returns at most.
const PageSize = 50

// List returns the workspace's newest PageSize entries viewerID may see with ids below before ("" from the newest)
// whose action starts with one of prefixes (nil: any), and whether there are more. A project's entries show only to
// who can see the project; a deleted project's only to admins.
func List(ctx context.Context, db *pgxpool.Pool, workspaceID, viewerID, before string, prefixes []string) ([]Entry, bool, error) {
	if prefixes == nil {
		prefixes = []string{}
	}
	rows, err := db.Query(ctx,
		`select id::text, at, actor_id::text, actor_name, actor_email, action, target, detail, ip from workspace_audit
		  where workspace_id = $1 and ($2::uuid is null or id < $2::uuid)
		    and (cardinality($3::text[]) = 0 or split_part(action, '.', 1) = any($3))
		    and (project_id is null or coalesce(can_see_project(project_id, $5), exists (
		          select 1 from workspace_members m join workspace_roles r on r.id = m.role_id
		           where m.workspace_id = $1 and m.user_id = $5 and r.builtin = 'admin')))
		  order by id desc limit $4`, workspaceID, nullable(before), prefixes, PageSize+1, viewerID)
	if err != nil {
		return nil, false, fmt.Errorf("list audit: %w", err)
	}
	es, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Entry, error) {
		var e Entry
		var a Actor
		err := row.Scan(&e.ID, &e.At, &a.ID, &a.Name, &a.Email, &e.Action, &e.Target, &e.Detail, &e.IP)
		if a.ID != nil || a.Name != "" || a.Email != "" {
			e.Actor = &a
		}
		e.At = e.At.UTC()
		return e, err
	})
	if err != nil {
		return nil, false, fmt.Errorf("list audit: %w", err)
	}
	if len(es) > PageSize {
		return es[:PageSize], true, nil
	}
	return es, false, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
