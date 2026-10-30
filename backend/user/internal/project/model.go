package project

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var errTaken = errors.New("project key taken")

type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Key         string    `json:"key"`
	Description string    `json:"description"`
	Archived    bool      `json:"archived"`
	CreatedAt   time.Time `json:"created_at"`
}

const cols = `id::text, name, key, description, archived_at is not null, created_at`

func scanProject(row pgx.Row) (Project, error) {
	var p Project
	err := row.Scan(&p.ID, &p.Name, &p.Key, &p.Description, &p.Archived, &p.CreatedAt)
	p.CreatedAt = p.CreatedAt.UTC()
	return p, err
}

// model owns every query for projects.
type model struct{ db *pgxpool.Pool }

// list returns the workspace's active projects by name, or its archived ones most recently archived first.
func (m *model) list(ctx context.Context, workspaceID string, archived bool) ([]Project, error) {
	rows, err := m.db.Query(ctx,
		`select `+cols+` from projects
		  where workspace_id = $1 and (archived_at is not null) = $2
		  order by archived_at desc, lower(name), key`, workspaceID, archived)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	ps, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Project, error) { return scanProject(row) })
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	return ps, nil
}

// create adds a project. Returns errTaken when the key is in use in the workspace.
func (m *model) create(ctx context.Context, workspaceID, userID, name, key, description string) (Project, error) {
	p, err := scanProject(m.db.QueryRow(ctx,
		`insert into projects (workspace_id, created_by, name, key, description) values ($1, $2, $3, $4, $5)
		 returning `+cols, workspaceID, userID, name, key, description))
	if isUniqueViolation(err) {
		return p, errTaken
	}
	if err != nil {
		return p, fmt.Errorf("create project: %w", err)
	}
	return p, nil
}

// get returns the project with the key; nil when there's none.
func (m *model) get(ctx context.Context, workspaceID, key string) (*Project, error) {
	return one(m.db.QueryRow(ctx, `select `+cols+` from projects where workspace_id = $1 and key = canonical_key($1, $2)`, workspaceID, key))
}

// update changes the project's name, key and description; nil when there's no project with the key.
// Returns errTaken when the new key belongs to another project.
func (m *model) update(ctx context.Context, workspaceID, key, name, newKey, description string) (*Project, error) {
	p, err := one(m.db.QueryRow(ctx,
		`update projects set name = $3, key = $4, description = $5 where workspace_id = $1 and key = canonical_key($1, $2) returning `+cols,
		workspaceID, key, name, newKey, description))
	if isUniqueViolation(err) {
		return nil, errTaken
	}
	return p, err
}

// setArchived archives or unarchives the project, keeping the original archive time when it's already archived;
// nil when there's no project with the key.
func (m *model) setArchived(ctx context.Context, workspaceID, key string, archived bool) (*Project, error) {
	return one(m.db.QueryRow(ctx,
		`update projects set archived_at = case when $3 then coalesce(archived_at, now()) end
		  where workspace_id = $1 and key = canonical_key($1, $2) returning `+cols, workspaceID, key, archived))
}

// one scans a single project; nil when there's no row.
func one(row pgx.Row) (*Project, error) {
	p, err := scanProject(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("project: %w", err)
	}
	return &p, nil
}

func isUniqueViolation(err error) bool {
	pgErr := (*pgconn.PgError)(nil)
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
