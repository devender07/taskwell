package project

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/user/internal/cycle"
	"github.com/devender07/tether/backend/user/internal/db"
	"github.com/devender07/tether/backend/user/internal/notify"
	"github.com/devender07/tether/backend/user/internal/realtime"
	"github.com/devender07/tether/backend/user/internal/web"
	"github.com/devender07/tether/backend/user/internal/workspace"
)

var (
	errTaken       = errors.New("project key taken")
	errNotMember   = errors.New("not a workspace member")
	errPublic      = errors.New("project is public")
	errDates       = errors.New("target date before start date")
	errNoProject   = errors.New("project not found")
	errArchived    = errors.New("project archived")
	errActive      = errors.New("project isn't archived")
	errNoMilestone = errors.New("milestone not found")
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
	StartDate   *string   `json:"start_date"`
	TargetDate  *string   `json:"target_date"`
	CreatedAt   time.Time `json:"created_at"`
	ManualOrder bool      `json:"manual_order"` // someone has ordered its issues by hand: its lists default to manual
}

// input is a create or update form. A nil date is unchanged on update (unset on create); "" clears it.
type input struct {
	name, key, description string
	start, target          *string
}

// cols selects a Project from projects (not aliased).
const cols = `id::text, name, key, description, archived_at is not null, private,
	coalesce((select json_agg(json_build_object('id', u.id, 'name', u.name) order by lower(u.name), u.id)
	  from project_members pm join users u on u.id = pm.user_id where pm.project_id = projects.id), '[]'),
	to_char(start_date, 'YYYY-MM-DD'), to_char(target_date, 'YYYY-MM-DD'), created_at, manual_order`

// visible is the condition that the viewer ($2 unless placed otherwise) can see the project.
const visible = `can_see_project(id, $2)`

func scanProject(row pgx.Row) (Project, error) {
	var p Project
	err := row.Scan(&p.ID, &p.Name, &p.Key, &p.Description, &p.Archived, &p.Private, &p.Members, &p.StartDate, &p.TargetDate, &p.CreatedAt, &p.ManualOrder)
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
	e.Paths = []string{realtime.Workspace(e.Workspace) + "/projects", realtime.MyIssues(e.Workspace), realtime.Watching(e.Workspace),
		realtime.ProjectStats(e.Workspace), realtime.Roadmap(e.Workspace), realtime.Activity(e.Workspace)} // archiving hides its issues from the feed
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
	if slices.ContainsFunc(ids, func(id string) bool { return !web.IsUUID(id) }) {
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

// addMembers makes the users members of the project, added by the actor, and notifies the ones who weren't already
// (except the actor).
func addMembers(ctx context.Context, tx pgx.Tx, projectID, actorID string, ids []string) error {
	rows, err := tx.Query(ctx, `insert into project_members (project_id, user_id, added_by)
		select $1, u, $2 from unnest($3::uuid[]) u on conflict do nothing returning user_id::text`, projectID, actorID, ids)
	if err != nil {
		return err
	}
	added, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	return notify.RecordProjectAdded(ctx, tx, projectID, actorID, added)
}

// create adds a project and publishes e; a private one gets the creator and memberIDs (workspace members) as
// members. Returns errTaken when the key is in use in the workspace, errNotMember for a member id that isn't one,
// errDates when the target date is before the start date.
func (m *model) create(ctx context.Context, workspaceID, userID string, in input, private bool, memberIDs []string,
	e realtime.Event) (Project, error) {
	var p Project
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if err := checkMembers(ctx, tx, workspaceID, memberIDs); err != nil {
			return err
		}
		var id string
		if err := tx.QueryRow(ctx,
			`insert into projects (workspace_id, created_by, name, key, description, private, start_date, target_date)
			 values ($1, $2, $3, $4, $5, $6, nullif($7, '')::date, nullif($8, '')::date)
			 returning id::text`, workspaceID, userID, in.name, in.key, in.description, private, in.start, in.target).Scan(&id); err != nil {
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
	if db.IsUniqueViolation(err) {
		return p, errTaken
	}
	if db.IsCheckViolation(err, "projects_dates") {
		return p, errDates
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

// update changes the project's name, key, description and, unless nil, dates and privacy, and publishes e; nil when
// the actor can't see a project with the key. Going private makes the actor a member; going public drops the members.
// Returns errTaken when the new key belongs to another project, errDates when the target date ends up before the start.
func (m *model) update(ctx context.Context, workspaceID, key, actorID string, in input, private *bool,
	e realtime.Event) (*Project, error) {
	var p *Project
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		id, old, wasPrivate, err := find(ctx, tx, workspaceID, key, actorID)
		if err != nil || id == "" {
			return err
		}
		if _, err := tx.Exec(ctx, `update projects set name = $2, key = $3, description = $4, private = coalesce($5, private),
			start_date = case when $6::text is null then start_date else nullif($6, '')::date end,
			target_date = case when $7::text is null then target_date else nullif($7, '')::date end where id = $1`,
			id, in.name, in.key, in.description, private, in.start, in.target); err != nil {
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
	if db.IsUniqueViolation(err) {
		return nil, errTaken
	}
	if db.IsCheckViolation(err, "projects_dates") {
		return nil, errDates
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

// remove deletes the archived project the actor can see, with everything in it (issues, cycles, milestones cascade),
// and tells the whole workspace to refetch. Returns false when there's no such project, errActive for one that isn't
// archived.
func (m *model) remove(ctx context.Context, workspaceID, key, actorID string, e realtime.Event) (bool, error) {
	found := false
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var id, current string
		var archived bool
		err := tx.QueryRow(ctx, `select id::text, key, archived_at is not null from projects
			where workspace_id = $1 and `+visible+` and key = canonical_key($1, $3) for update`,
			workspaceID, actorID, key).Scan(&id, &current, &archived)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		if !archived {
			return errActive
		}
		if _, err := tx.Exec(ctx, `delete from projects where id = $1`, id); err != nil {
			return err
		}
		if err := changed(ctx, tx, e, id, true, current); err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, realtime.AdminStats(actorID))
	})
	if err != nil && !errors.Is(err, errActive) {
		return found, fmt.Errorf("delete project: %w", err)
	}
	return found, err
}

// people returns the workspace members who can see the project with the key whose name or email contains q, ordered
// like the workspace's members, at most limit (0: all); nil when the viewer can't see it.
func (m *model) people(ctx context.Context, workspaceID, key, viewerID, q string, limit int) ([]workspace.Member, error) {
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
			    and (strpos(lower(u.name), lower($3)) > 0 or strpos(lower(u.email), lower($3)) > 0)
			  order by r.builtin is not distinct from 'admin' desc, lower(u.name), u.email
			  limit nullif($4, 0)`, workspaceID, id, q, limit)
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
			ids = slices.DeleteFunc(ids, func(id string) bool { return !web.IsUUID(id) })
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

// --- milestones ---

type Milestone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Date string `json:"date"`
}

const (
	milestoneCols  = `id::text, name, to_char(date, 'YYYY-MM-DD')`
	milestoneOrder = `order by date, lower(name), id`
)

// milestones returns the milestones of the project with the key by date, then name; nil when the viewer can't see it.
func (m *model) milestones(ctx context.Context, workspaceID, key, viewerID string) ([]Milestone, error) {
	var id string
	err := m.db.QueryRow(ctx, `select id::text from projects where workspace_id = $1 and `+visible+`
		and key = canonical_key($1, $3)`, workspaceID, viewerID, key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("milestones: %w", err)
	}
	rows, err := m.db.Query(ctx, `select `+milestoneCols+` from project_milestones where project_id = $1 `+milestoneOrder, id)
	if err != nil {
		return nil, fmt.Errorf("milestones: %w", err)
	}
	ms, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Milestone])
	if err != nil {
		return nil, fmt.Errorf("milestones: %w", err)
	}
	return ms, nil
}

// writeMilestone locks the project with the key the actor can see, runs fn with its id and publishes e:
// errNoProject when there's no such project, errArchived when it's archived.
func (m *model) writeMilestone(ctx context.Context, workspaceID, key, actorID string, e realtime.Event,
	fn func(tx pgx.Tx, projectID string) error) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var id, current string
		var archived bool
		err := tx.QueryRow(ctx, `select id::text, key, archived_at is not null from projects where workspace_id = $1 and `+visible+`
			and key = canonical_key($1, $3) for update`, workspaceID, actorID, key).Scan(&id, &current, &archived)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return errNoProject
		case err != nil:
			return err
		case archived:
			return errArchived
		}
		if err := fn(tx, id); err != nil {
			return err
		}
		e.Kind, e.Project = "milestone.changed", id
		e.Paths = []string{realtime.Roadmap(e.Workspace), realtime.Milestones(e.Workspace, current), realtime.Project(e.Workspace, current)}
		return realtime.Publish(ctx, tx, e)
	})
	if err == nil || errors.Is(err, errNoProject) || errors.Is(err, errArchived) || errors.Is(err, errNoMilestone) {
		return err
	}
	return fmt.Errorf("write milestone: %w", err)
}

func (m *model) createMilestone(ctx context.Context, workspaceID, key, actorID, name, date string, e realtime.Event) (*Milestone, error) {
	var ms Milestone
	err := m.writeMilestone(ctx, workspaceID, key, actorID, e, func(tx pgx.Tx, projectID string) error {
		return tx.QueryRow(ctx, `insert into project_milestones (project_id, name, date, created_by) values ($1, $2, $3::date, $4)
			returning `+milestoneCols, projectID, name, date, actorID).Scan(&ms.ID, &ms.Name, &ms.Date)
	})
	return &ms, err
}

// updateMilestone renames and redates the project's milestone with the id (a UUID); errNoMilestone when there's none.
func (m *model) updateMilestone(ctx context.Context, workspaceID, key, actorID, id, name, date string, e realtime.Event) (*Milestone, error) {
	var ms Milestone
	err := m.writeMilestone(ctx, workspaceID, key, actorID, e, func(tx pgx.Tx, projectID string) error {
		err := tx.QueryRow(ctx, `update project_milestones set name = $3, date = $4::date where id = $1 and project_id = $2
			returning `+milestoneCols, id, projectID, name, date).Scan(&ms.ID, &ms.Name, &ms.Date)
		if errors.Is(err, pgx.ErrNoRows) {
			return errNoMilestone
		}
		return err
	})
	return &ms, err
}

// deleteMilestone removes the project's milestone with the id (a UUID); errNoMilestone when there's none.
func (m *model) deleteMilestone(ctx context.Context, workspaceID, key, actorID, id string, e realtime.Event) error {
	return m.writeMilestone(ctx, workspaceID, key, actorID, e, func(tx pgx.Tx, projectID string) error {
		tag, err := tx.Exec(ctx, `delete from project_milestones where id = $1 and project_id = $2`, id, projectID)
		if err == nil && tag.RowsAffected() == 0 {
			return errNoMilestone
		}
		return err
	})
}

// --- roadmap ---

type RoadmapProject struct {
	id         string
	Key        string  `json:"key"`
	Name       string  `json:"name"`
	Private    bool    `json:"private"`
	StartDate  *string `json:"start_date"`
	TargetDate *string `json:"target_date"`
	Progress   struct {
		Total     int `json:"total"`
		Completed int `json:"completed"`
		Canceled  int `json:"canceled"`
	} `json:"progress"`
	Milestones []Milestone    `json:"milestones"`
	Cycles     []RoadmapCycle `json:"cycles"`
}

// RoadmapCycle is a cycle as the roadmap shows it; State is as in the cycle package.
type RoadmapCycle struct {
	Number   int    `json:"number"`
	Name     string `json:"name"`
	StartsOn string `json:"starts_on"`
	EndsOn   string `json:"ends_on"`
	State    string `json:"state"`
}

// roadmap returns the workspace's active projects the viewer can see, by start date (or target date when there's
// none, undated last) then name, with their issue counts, milestones and cycles.
func (m *model) roadmap(ctx context.Context, workspaceID, viewerID string) ([]RoadmapProject, error) {
	rows, err := m.db.Query(ctx,
		`select p.id::text, p.key, p.name, p.private, to_char(p.start_date, 'YYYY-MM-DD'), to_char(p.target_date, 'YYYY-MM-DD'),
		        count(i.id), count(*) filter (where s.category = 'completed'), count(*) filter (where s.category = 'canceled')
		 from projects p left join issues i on i.project_id = p.id left join statuses s on s.id = i.status_id
		 where p.workspace_id = $1 and p.archived_at is null and can_see_project(p.id, $2)
		 group by p.id order by coalesce(p.start_date, p.target_date) nulls last, lower(p.name), p.key`, workspaceID, viewerID)
	if err != nil {
		return nil, fmt.Errorf("roadmap: %w", err)
	}
	ps, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (RoadmapProject, error) {
		p := RoadmapProject{Milestones: []Milestone{}, Cycles: []RoadmapCycle{}}
		err := row.Scan(&p.id, &p.Key, &p.Name, &p.Private, &p.StartDate, &p.TargetDate,
			&p.Progress.Total, &p.Progress.Completed, &p.Progress.Canceled)
		return p, err
	})
	if err != nil {
		return nil, fmt.Errorf("roadmap: %w", err)
	}
	at := make(map[string]*RoadmapProject, len(ps))
	ids := make([]string, len(ps))
	for i := range ps {
		at[ps[i].id], ids[i] = &ps[i], ps[i].id
	}

	var projectID string
	var ms Milestone
	rows, err = m.db.Query(ctx, `select project_id::text, `+milestoneCols+` from project_milestones
		where project_id = any($1::uuid[]) `+milestoneOrder, ids)
	if err != nil {
		return nil, fmt.Errorf("roadmap milestones: %w", err)
	}
	if _, err := pgx.ForEachRow(rows, []any{&projectID, &ms.ID, &ms.Name, &ms.Date}, func() error {
		at[projectID].Milestones = append(at[projectID].Milestones, ms)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("roadmap milestones: %w", err)
	}

	var c RoadmapCycle
	rows, err = m.db.Query(ctx, `select c.project_id::text, c.number, coalesce(c.name, 'Cycle ' || c.number),
		to_char(c.starts_on, 'YYYY-MM-DD'), to_char(c.ends_on, 'YYYY-MM-DD'), `+cycle.StateSQL+`
		from cycles c where c.project_id = any($1::uuid[]) order by c.starts_on, c.number`, ids)
	if err != nil {
		return nil, fmt.Errorf("roadmap cycles: %w", err)
	}
	if _, err := pgx.ForEachRow(rows, []any{&projectID, &c.Number, &c.Name, &c.StartsOn, &c.EndsOn, &c.State}, func() error {
		at[projectID].Cycles = append(at[projectID].Cycles, c)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("roadmap cycles: %w", err)
	}
	return ps, nil
}
