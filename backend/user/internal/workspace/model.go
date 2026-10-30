package workspace

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	roleAdmin  = "admin"
	roleMember = "member"
)

var errTaken = errors.New("workspace slug taken")

// Workspace is a workspace as seen by one member; Role is that member's role.
type Workspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
	Role string `json:"role"`
}

// model owns every query for workspaces and their members.
type model struct{ db *pgxpool.Pool }

// list returns the user's workspaces, most recently opened first.
func (m *model) list(ctx context.Context, userID string) ([]Workspace, error) {
	rows, err := m.db.Query(ctx,
		`select w.id::text, w.name, w.slug, m.role
		   from workspace_members m join workspaces w on w.id = m.workspace_id
		  where m.user_id = $1
		  order by m.last_opened_at desc, w.created_at desc`, userID)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	ws, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Workspace])
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	return ws, nil
}

// create inserts the workspace and makes the user its admin. Returns errTaken on a duplicate slug.
func (m *model) create(ctx context.Context, userID, name, slug string) (Workspace, error) {
	ws := Workspace{Name: name, Slug: slug, Role: roleAdmin}
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			`insert into workspaces (name, slug, created_by) values ($1, $2, $3) returning id::text`,
			name, slug, userID).Scan(&ws.ID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`insert into workspace_members (workspace_id, user_id, role) values ($1, $2, $3)`, ws.ID, userID, roleAdmin)
		return err
	})
	if isUniqueViolation(err) {
		return Workspace{}, errTaken
	}
	if err != nil {
		return Workspace{}, fmt.Errorf("create workspace: %w", err)
	}
	return ws, nil
}

// open marks the workspace as opened now by the user and returns it; nil when it's missing or the user isn't a member.
func (m *model) open(ctx context.Context, userID, slug string) (*Workspace, error) {
	var ws Workspace
	err := m.db.QueryRow(ctx,
		`update workspace_members m set last_opened_at = now()
		   from workspaces w
		  where w.id = m.workspace_id and w.slug = $1 and m.user_id = $2
		  returning w.id::text, w.name, w.slug, m.role`, slug, userID).Scan(&ws.ID, &ws.Name, &ws.Slug, &ws.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open workspace: %w", err)
	}
	return &ws, nil
}

// role returns the user's role in the workspace, or "" when it's missing or the user isn't a member.
func (m *model) role(ctx context.Context, userID, slug string) (string, error) {
	var role string
	err := m.db.QueryRow(ctx,
		`select m.role from workspace_members m join workspaces w on w.id = m.workspace_id
		  where w.slug = $1 and m.user_id = $2`, slug, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("workspace role: %w", err)
	}
	return role, nil
}

// update renames the workspace and changes its slug if the user is its admin; nil when they aren't (any more).
// Returns errTaken when the new slug belongs to another workspace.
func (m *model) update(ctx context.Context, userID, slug, newName, newSlug string) (*Workspace, error) {
	ws := Workspace{Name: newName, Slug: newSlug, Role: roleAdmin}
	err := m.db.QueryRow(ctx,
		`update workspaces w set name = $3, slug = $4
		  where w.slug = $1 and exists (select 1 from workspace_members m
		         where m.workspace_id = w.id and m.user_id = $2 and m.role = 'admin')
		  returning w.id::text`, slug, userID, newName, newSlug).Scan(&ws.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if isUniqueViolation(err) {
		return nil, errTaken
	}
	if err != nil {
		return nil, fmt.Errorf("update workspace: %w", err)
	}
	return &ws, nil
}

func isUniqueViolation(err error) bool {
	pgErr := (*pgconn.PgError)(nil)
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// delete removes the workspace when userID is one of its admins; members go with it (on delete cascade).
func (m *model) delete(ctx context.Context, userID, slug string) (bool, error) {
	tag, err := m.db.Exec(ctx,
		`delete from workspaces w
		  where w.slug = $1 and exists (select 1 from workspace_members m
		         where m.workspace_id = w.id and m.user_id = $2 and m.role = 'admin')`, slug, userID)
	if err != nil {
		return false, fmt.Errorf("delete workspace: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}
