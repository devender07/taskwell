package view

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/realtime"
	"github.com/devender07/taskwell/backend/user/internal/web"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

// maxViews caps a member's views in a workspace.
const maxViews = 50

var errTooMany = errors.New("too many views")

type Person struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// View is a saved view; Path is relative to the workspace (/projects/WEB?view=board).
type View struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Shared  bool   `json:"shared"`
	Owner   Person `json:"owner"`
	CanEdit bool   `json:"can_edit"`
}

// viewCols select a View from the views v (a table or a with query).
const viewCols = `v.id::text, v.name, v.path, v.shared, u.id::text, u.name from %s v join users u on u.id = v.user_id`

// views selects from the saved_views table.
var views = fmt.Sprintf(viewCols, "saved_views")

// visible is where for the views the user ($2) sees in the workspace ($1): their own and the shared ones (not for
// guests: a shared path can name projects they can't see), less shared project views of projects they can't see.
const visible = `v.workspace_id = $1 and (v.user_id = $2 or v.shared and not is_guest($1, $2) and (v.path not like '/projects/%'
	or exists (select 1 from projects p where p.workspace_id = $1 and can_see_project(p.id, $2)
	  and p.key = canonical_key($1, upper(split_part(split_part(v.path, '?', 1), '/', 3))))))`

func scanView(row pgx.Row) (View, error) {
	var v View
	err := row.Scan(&v.ID, &v.Name, &v.Path, &v.Shared, &v.Owner.ID, &v.Owner.Name)
	return v, err
}

// model owns every query for saved views.
type model struct{ db *pgxpool.Pool }

// list returns the views the user sees, by name.
func (m *model) list(ctx context.Context, workspaceID, userID string) ([]View, error) {
	rows, err := m.db.Query(ctx, `select `+views+` where `+visible+` order by lower(v.name), v.id`, workspaceID, userID)
	if err != nil {
		return nil, fmt.Errorf("list views: %w", err)
	}
	vs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (View, error) { return scanView(row) })
	if err != nil {
		return nil, fmt.Errorf("list views: %w", err)
	}
	return vs, nil
}

// get returns the view with the id; nil when there's none or the user doesn't see it.
func (m *model) get(ctx context.Context, workspaceID, id, userID string) (*View, error) {
	return oneView(m.db.QueryRow(ctx, `select `+views+` where `+visible+` and v.id = $3`, workspaceID, userID, nullUUID(id)))
}

// create adds the user's view, returning errTooMany when they have maxViews in the workspace already.
func (m *model) create(ctx context.Context, ws *workspace.Workspace, userID, name, path string, shared bool) (*View, error) {
	var v *View
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		// Locking their membership makes concurrent creates count in turn.
		var n int
		if err := tx.QueryRow(ctx,
			`select (select count(*) from saved_views where workspace_id = $1 and user_id = $2)
			 from workspace_members where workspace_id = $1 and user_id = $2 for update`, ws.ID, userID).Scan(&n); err != nil {
			return err
		}
		if n >= maxViews {
			return errTooMany
		}
		var err error
		if v, err = oneView(tx.QueryRow(ctx,
			`with v as (insert into saved_views (workspace_id, user_id, name, path, shared) values ($1, $2, $3, $4, $5) returning *)
			 select `+fmt.Sprintf(viewCols, "v"), ws.ID, userID, name, path, shared)); err != nil {
			return err
		}
		return publish(ctx, tx, ws, userID, v, shared)
	})
	if errors.Is(err, errTooMany) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("create view: %w", err)
	}
	return v, nil
}

// update sets the view's non-nil fields and returns it as changed.
func (m *model) update(ctx context.Context, ws *workspace.Workspace, actorID string, before *View, name, path *string, shared *bool) (*View, error) {
	var v *View
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var err error
		if v, err = oneView(tx.QueryRow(ctx,
			`with v as (update saved_views set name = coalesce($2, name), path = coalesce($3, path), shared = coalesce($4, shared)
			            where id = $1 returning *)
			 select `+fmt.Sprintf(viewCols, "v"), before.ID, name, path, shared)); err != nil || v == nil {
			return err
		}
		return publish(ctx, tx, ws, actorID, v, before.Shared)
	})
	if err != nil {
		return nil, fmt.Errorf("update view: %w", err)
	}
	if v == nil {
		return before, nil // deleted meanwhile
	}
	return v, nil
}

func (m *model) delete(ctx context.Context, ws *workspace.Workspace, actorID string, v *View) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `delete from saved_views where id = $1`, v.ID); err != nil {
			return err
		}
		return publish(ctx, tx, ws, actorID, v, v.Shared)
	})
	if err != nil {
		return fmt.Errorf("delete view: %w", err)
	}
	return nil
}

// publish tells the workspace its views changed when the view is or was shared (wasShared), else only its owner.
func publish(ctx context.Context, tx pgx.Tx, ws *workspace.Workspace, actorID string, v *View, wasShared bool) error {
	e := realtime.Event{Kind: "views.changed", Workspace: ws.Slug, Actor: actorID, Audience: realtime.ToUser(v.Owner.ID),
		Paths: []string{realtime.Views(ws.Slug)}}
	if v.Shared || wasShared {
		e.Audience = realtime.ToWorkspace(ws.ID)
	}
	return realtime.Publish(ctx, tx, e)
}

func oneView(row pgx.Row) (*View, error) {
	v, err := scanView(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("view: %w", err)
	}
	return &v, nil
}

// nullUUID is id, or nil when it isn't a uuid (so it matches nothing instead of failing the cast).
func nullUUID(id string) *string {
	if !web.IsUUID(id) {
		return nil
	}
	return &id
}
