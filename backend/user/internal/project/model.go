package project

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/user/internal/realtime"
	"github.com/devender07/tether/backend/user/internal/workspace"
)

var (
	errTaken     = errors.New("project key taken")
	errNotMember = errors.New("not a workspace member")
	errPublic    = errors.New("project is public")
)

type Person struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Key         string    `json:"key"`
	Description string    `json:"description"`
	Archived    bool      `json:"archived"`
	Private     bool      `json:"private"`
	Members     []Person  `json:"members"` // a private project's explicit members by name; [] for a public one
	CreatedAt   time.Time `json:"created_at"`
}

// cols selects a Project from projects (not aliased).
const cols = `id::text, name, key, description, archived_at is not null, private,
	coalesce((select json_agg(json_build_object('id', u.id, 'name', u.name) order by lower(u.name), u.id)
	  from project_members pm join users u on u.id = pm.user_id where pm.project_id = projects.id), '[]'), created_at`

// visible is the condition that the viewer ($2 unless placed otherwise) can see the project.
const visible = `can_see_project(id, $2)`

func scanProject(row pgx.Row) (Project, error) {
	var p Project
	err := row.Scan(&p.ID, &p.Name, &p.Key, &p.Description, &p.Archived, &p.Private, &p.Members, &p.CreatedAt)
	p.CreatedAt = p.CreatedAt.UTC()
	return p, err
}

// model owns every query for projects.
type model struct{ db *pgxpool.Pool }

// list returns the workspace's active projects the viewer can see by name, or its archived ones most recently
// archived first.
func (m *model) list(ctx context.Context, workspaceID, viewerID string, archived bool) ([]Project, error) {
	rows, err := m.db.Query(ctx,
		`select `+cols+` from projects
		  where workspace_id = $1 and `+visible+` and (archived_at is not null) = $3
		  order by archived_at desc, lower(name), key`, workspaceID, viewerID, archived)
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

// stats returns the counts of every active project the viewer can see, in list's order.
func (m *model) stats(ctx context.Context, workspaceID, viewerID string) ([]Stats, error) {
	rows, err := m.db.Query(ctx,
		`select p.key,
		        count(*) filter (where s.category in ('backlog', 'unstarted', 'started')),
		        count(*) filter (where s.category = 'completed'),
		        count(*) filter (where s.category = 'canceled'),
		        count(*) filter (where s.category in ('backlog', 'unstarted', 'started') and i.due_date < current_date)
		 from projects p left join issues i on i.project_id = p.id left join statuses s on s.id = i.status_id
		 where p.workspace_id = $1 and p.archived_at is null and can_see_project(p.id, $2)
		 group by p.id order by lower(p.name), p.key`, workspaceID, viewerID)
	if err != nil {
		return nil, fmt.Errorf("project stats: %w", err)
	}
	ss, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Stats])
	if err != nil {
		return nil, fmt.Errorf("project stats: %w", err)
	}
	return ss, nil
}

// changed publishes project.changed for the project with the id and keys (old and new) in tx: to the people who can
// see it, or to the whole workspace with every path when everyone must refetch (its visibility changed).
func changed(ctx context.Context, tx pgx.Tx, e realtime.Event, projectID string, everyone bool, keys ...string) error {
	e.Kind = "project.changed"
	e.Paths = []string{realtime.Workspace(e.Workspace) + "/projects", realtime.MyIssues(e.Workspace),
		realtime.ProjectStats(e.Workspace), realtime.Activity(e.Workspace)} // archiving hides its issues from the feed
	for _, k := range keys {
		e.Paths = append(e.Paths, realtime.Project(e.Workspace, k), realtime.ProjectIssues(e.Workspace, k))
	}
	if everyone {
		e.Paths = append(e.Paths, realtime.All(e.Workspace))
	} else {
		e.Project = projectID
	}
	return realtime.Publish(ctx, tx, e)
}

// checkMembers returns errNotMember unless every id is a member of the workspace.
func checkMembers(ctx context.Context, tx pgx.Tx, workspaceID string, ids []string) error {
	if slices.ContainsFunc(ids, func(id string) bool { return !isUUID(id) }) {
		return errNotMember
	}
	var n int
	if err := tx.QueryRow(ctx, `select count(*) from workspace_members where workspace_id = $1 and user_id = any($2::uuid[])`,
		workspaceID, ids).Scan(&n); err != nil {
		return err
	}
	if n != len(ids) {
		return errNotMember
	}
	return nil
}

// addMembers makes the users members of the project, added by the actor.
func addMembers(ctx context.Context, tx pgx.Tx, projectID, actorID string, ids []string) error {
	_, err := tx.Exec(ctx, `insert into project_members (project_id, user_id, added_by)
		select $1, u, $2 from unnest($3::uuid[]) u on conflict do nothing`, projectID, actorID, ids)
	return err
}

// create adds a project and publishes e; a private one gets the creator and memberIDs (workspace members) as
// members. Returns errTaken when the key is in use in the workspace, errNotMember for a member id that isn't one.
func (m *model) create(ctx context.Context, workspaceID, userID, name, key, description string, private bool, memberIDs []string,
	e realtime.Event) (Project, error) {
	var p Project
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if err := checkMembers(ctx, tx, workspaceID, memberIDs); err != nil {
			return err
		}
		var id string
		if err := tx.QueryRow(ctx,
			`insert into projects (workspace_id, created_by, name, key, description, private) values ($1, $2, $3, $4, $5, $6)
			 returning id::text`, workspaceID, userID, name, key, description, private).Scan(&id); err != nil {
			return err
		}
		if private {
			if err := addMembers(ctx, tx, id, userID, append(memberIDs, userID)); err != nil {
				return err
			}
		}
		var err error
		if p, err = scanProject(tx.QueryRow(ctx, `select `+cols+` from projects where id = $1`, id)); err != nil {
			return err
		}
		if err := changed(ctx, tx, e, p.ID, false, p.Key); err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, realtime.AdminStats(userID))
	})
	if isUniqueViolation(err) {
		return p, errTaken
	}
	if errors.Is(err, errNotMember) {
		return p, err
	}
	if err != nil {
		return p, fmt.Errorf("create project: %w", err)
	}
	return p, nil
}

// get returns the project with the key when the viewer can see it; nil otherwise.
func (m *model) get(ctx context.Context, workspaceID, key, viewerID string) (*Project, error) {
	return one(m.db.QueryRow(ctx, `select `+cols+` from projects where workspace_id = $1 and `+visible+`
		and key = canonical_key($1, $3)`, workspaceID, viewerID, key))
}

// find locks the project with the key the viewer can see and returns its id, key and privacy; id "" when there's none.
func find(ctx context.Context, tx pgx.Tx, workspaceID, key, viewerID string) (id, current string, private bool, err error) {
	err = tx.QueryRow(ctx, `select id::text, key, private from projects where workspace_id = $1 and `+visible+`
		and key = canonical_key($1, $3) for update`, workspaceID, viewerID, key).Scan(&id, &current, &private)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, nil
	}
	return id, current, private, err
}

// update changes the project's name, key, description and, unless nil, privacy, and publishes e; nil when the
// actor can't see a project with the key. Going private makes the actor a member; going public drops the members.
// Returns errTaken when the new key belongs to another project.
func (m *model) update(ctx context.Context, workspaceID, key, actorID, name, newKey, description string, private *bool,
	e realtime.Event) (*Project, error) {
	var p *Project
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		id, old, wasPrivate, err := find(ctx, tx, workspaceID, key, actorID)
		if err != nil || id == "" {
			return err
		}
		if _, err := tx.Exec(ctx, `update projects set name = $2, key = $3, description = $4, private = coalesce($5, private) where id = $1`,
			id, name, newKey, description, private); err != nil {
			return err
		}
		flipped := private != nil && *private != wasPrivate
		switch {
		case flipped && *private:
			err = addMembers(ctx, tx, id, actorID, []string{actorID})
		case flipped:
			_, err = tx.Exec(ctx, `delete from project_members where project_id = $1`, id)
		}
		if err != nil {
			return err
		}
		if p, err = one(tx.QueryRow(ctx, `select `+cols+` from projects where id = $1`, id)); err != nil {
			return err
		}
		return changed(ctx, tx, e, id, flipped, old, p.Key)
	})
	if isUniqueViolation(err) {
		return nil, errTaken
	}
	return p, err
}

// setArchived archives or unarchives the project the actor can see, keeping the original archive time when it's
// already archived, and publishes e; nil when there's no such project.
func (m *model) setArchived(ctx context.Context, workspaceID, key, actorID string, archived bool, e realtime.Event) (*Project, error) {
	var p *Project
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var err error
		if p, err = one(tx.QueryRow(ctx,
			`update projects set archived_at = case when $4 then coalesce(archived_at, now()) end
			  where workspace_id = $1 and `+visible+` and key = canonical_key($1, $3) returning `+cols,
			workspaceID, actorID, key, archived)); err != nil || p == nil {
			return err
		}
		return changed(ctx, tx, e, p.ID, false, p.Key)
	})
	return p, err
}

// people returns the workspace members who can see the project with the key, ordered like the workspace's members;
// nil when the viewer can't see it.
func (m *model) people(ctx context.Context, workspaceID, key, viewerID string) ([]workspace.Member, error) {
	var ms []workspace.Member
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		id, _, _, err := find(ctx, tx, workspaceID, key, viewerID)
		if err != nil || id == "" {
			return err
		}
		rows, err := tx.Query(ctx,
			`select u.id::text, u.name, u.email, r.id::text, r.name
			   from workspace_members m join users u on u.id = m.user_id join workspace_roles r on r.id = m.role_id
			  where m.workspace_id = $1 and can_see_project($2, m.user_id)
			  order by r.builtin is not distinct from 'admin' desc, lower(u.name), u.email`, workspaceID, id)
		if err != nil {
			return err
		}
		ms, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (workspace.Member, error) {
			var mb workspace.Member
			err := row.Scan(&mb.ID, &mb.Name, &mb.Email, &mb.Role.ID, &mb.Role.Name)
			return mb, err
		})
		if ms == nil {
			ms = []workspace.Member{}
		}
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("project people: %w", err)
	}
	return ms, nil
}

// setMembers adds the users (workspace members) to the private project the actor can see, or removes them when add
// is false, and publishes e; nil when there's no such project. Returns errPublic for a public project and
// errNotMember for an id that isn't a workspace member. Someone removed is told to refetch everything, since the
// project is gone for them unless they're an admin.
func (m *model) setMembers(ctx context.Context, workspaceID, key, actorID string, ids []string, add bool, e realtime.Event) (*Project, error) {
	var p *Project
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		id, _, private, err := find(ctx, tx, workspaceID, key, actorID)
		switch {
		case err != nil || id == "":
			return err
		case !private:
			return errPublic
		}
		if add {
			if err := checkMembers(ctx, tx, workspaceID, ids); err != nil {
				return err
			}
			err = addMembers(ctx, tx, id, actorID, ids)
		} else {
			ids = slices.DeleteFunc(ids, func(id string) bool { return !isUUID(id) })
			_, err = tx.Exec(ctx, `delete from project_members where project_id = $1 and user_id = any($2::uuid[])`, id, ids)
		}
		if err != nil {
			return err
		}
		if p, err = one(tx.QueryRow(ctx, `select `+cols+` from projects where id = $1`, id)); err != nil {
			return err
		}
		if !add {
			for _, u := range ids {
				gone := e
				gone.Kind, gone.Audience, gone.Paths = "project.changed", realtime.ToUser(u), []string{realtime.All(e.Workspace)}
				if err := realtime.Publish(ctx, tx, gone); err != nil {
					return err
				}
			}
		}
		return changed(ctx, tx, e, id, false, p.Key)
	})
	if errors.Is(err, errPublic) || errors.Is(err, errNotMember) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("set project members: %w", err)
	}
	return p, nil
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
