package project

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/user/internal/realtime"
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

// Stats are an active project's issue counts by status category; overdue are open issues due before today.
type Stats struct {
	Key      string `json:"key"`
	Open     int    `json:"open"`
	Done     int    `json:"done"`
	Canceled int    `json:"canceled"`
	Overdue  int    `json:"overdue"`
}

// stats returns every active project's counts, in list's order.
func (m *model) stats(ctx context.Context, workspaceID string) ([]Stats, error) {
	rows, err := m.db.Query(ctx,
		`select p.key,
		        count(*) filter (where s.category in ('backlog', 'unstarted', 'started')),
		        count(*) filter (where s.category = 'completed'),
		        count(*) filter (where s.category = 'canceled'),
		        count(*) filter (where s.category in ('backlog', 'unstarted', 'started') and i.due_date < current_date)
		 from projects p left join issues i on i.project_id = p.id left join statuses s on s.id = i.status_id
		 where p.workspace_id = $1 and p.archived_at is null
		 group by p.id order by lower(p.name), p.key`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("project stats: %w", err)
	}
	ss, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Stats])
	if err != nil {
		return nil, fmt.Errorf("project stats: %w", err)
	}
	return ss, nil
}

// changed publishes project.changed for the projects with the keys (old and new) in tx, plus admin events.
func changed(ctx context.Context, tx pgx.Tx, e realtime.Event, keys ...string) error {
	e.Kind = "project.changed"
	e.Paths = []string{realtime.Workspace(e.Workspace) + "/projects", realtime.MyIssues(e.Workspace),
		realtime.ProjectStats(e.Workspace), realtime.Activity(e.Workspace)} // archiving hides its issues from the feed
	for _, k := range keys {
		e.Paths = append(e.Paths, realtime.Project(e.Workspace, k), realtime.ProjectIssues(e.Workspace, k))
	}
	return realtime.Publish(ctx, tx, e)
}

// create adds a project and publishes e. Returns errTaken when the key is in use in the workspace.
func (m *model) create(ctx context.Context, workspaceID, userID, name, key, description string, e realtime.Event) (Project, error) {
	var p Project
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var err error
		if p, err = scanProject(tx.QueryRow(ctx,
			`insert into projects (workspace_id, created_by, name, key, description) values ($1, $2, $3, $4, $5)
			 returning `+cols, workspaceID, userID, name, key, description)); err != nil {
			return err
		}
		if err := changed(ctx, tx, e, p.Key); err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, realtime.AdminStats(userID))
	})
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

// update changes the project's name, key and description and publishes e; nil when there's no project with the key.
// Returns errTaken when the new key belongs to another project.
func (m *model) update(ctx context.Context, workspaceID, key, name, newKey, description string, e realtime.Event) (*Project, error) {
	var p *Project
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var old string
		err := tx.QueryRow(ctx, `select key from projects where workspace_id = $1 and key = canonical_key($1, $2)`, workspaceID, key).Scan(&old)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if p, err = one(tx.QueryRow(ctx,
			`update projects set name = $3, key = $4, description = $5 where workspace_id = $1 and key = $2 returning `+cols,
			workspaceID, old, name, newKey, description)); err != nil || p == nil {
			return err
		}
		return changed(ctx, tx, e, old, p.Key)
	})
	if isUniqueViolation(err) {
		return nil, errTaken
	}
	return p, err
}

// setArchived archives or unarchives the project, keeping the original archive time when it's already archived, and
// publishes e; nil when there's no project with the key.
func (m *model) setArchived(ctx context.Context, workspaceID, key string, archived bool, e realtime.Event) (*Project, error) {
	var p *Project
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var err error
		if p, err = one(tx.QueryRow(ctx,
			`update projects set archived_at = case when $3 then coalesce(archived_at, now()) end
			  where workspace_id = $1 and key = canonical_key($1, $2) returning `+cols, workspaceID, key, archived)); err != nil || p == nil {
			return err
		}
		return changed(ctx, tx, e, p.Key)
	})
	return p, err
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
