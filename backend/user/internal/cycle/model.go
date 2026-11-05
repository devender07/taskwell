package cycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/realtime"
)

var (
	errNoProject = errors.New("project not found")
	errNoCycle   = errors.New("cycle not found")
	errArchived  = errors.New("project archived")
)

// fieldErrors are form messages found inside a write's transaction (overlaps, a bad rollover target).
type fieldErrors map[string]string

func (e fieldErrors) Error() string { return fmt.Sprint(map[string]string(e)) }

// Progress counts a cycle's issues by status category; backlog and unstarted are the rest of total.
type Progress struct {
	Total     int `json:"total"`
	Started   int `json:"started"`
	Completed int `json:"completed"`
	Canceled  int `json:"canceled"`
}

// Cycle is a project's cycle; Name is CustomName or "Cycle {number}". State is upcoming, current or past by today's
// date (UTC).
type Cycle struct {
	ID         string   `json:"id"`
	Number     int      `json:"number"`
	Name       string   `json:"name"`
	CustomName *string  `json:"custom_name"`
	StartsOn   string   `json:"starts_on"`
	EndsOn     string   `json:"ends_on"`
	State      string   `json:"state"`
	Progress   Progress `json:"progress"`
}

// input is a create or update form. A nil date is unchanged on update; name replaces the custom name (nil: the
// default) only when setName is true.
type input struct {
	setName          bool
	name             *string
	startsOn, endsOn *time.Time
}

// today is the current UTC date; StateSQL (the roadmap's too) matches the issue package's cycleJSON.
const (
	today    = `(now() at time zone 'utc')::date`
	StateSQL = `case when c.starts_on > ` + today + ` then 'upcoming' when c.ends_on < ` + today + ` then 'past' else 'current' end`
	cols     = `c.id::text, c.number, coalesce(c.name, 'Cycle ' || c.number), c.name, to_char(c.starts_on, 'YYYY-MM-DD'),
		to_char(c.ends_on, 'YYYY-MM-DD'), ` + StateSQL + `, count(i.id), count(*) filter (where s.category = 'started'),
		count(*) filter (where s.category = 'completed'), count(*) filter (where s.category = 'canceled')
		from cycles c left join issues i on i.cycle_id = c.id left join statuses s on s.id = i.status_id`
)

func scanCycle(row pgx.Row) (Cycle, error) {
	var c Cycle
	err := row.Scan(&c.ID, &c.Number, &c.Name, &c.CustomName, &c.StartsOn, &c.EndsOn, &c.State,
		&c.Progress.Total, &c.Progress.Started, &c.Progress.Completed, &c.Progress.Canceled)
	return c, err
}

// model owns every query for cycles.
type model struct{ db *pgxpool.Pool }

type project struct {
	id, key  string
	archived bool
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// findProject returns the workspace's project with the key (old keys too), locking it when lock is set; nil when
// there's none or the viewer can't see it.
func findProject(ctx context.Context, q querier, workspaceID, key, viewerID string, lock bool) (*project, error) {
	sql := `select id::text, key, archived_at is not null from projects where workspace_id = $1 and key = canonical_key($1, $2)
		and can_see_project(id, $3)`
	if lock {
		sql += ` for update`
	}
	var p project
	err := q.QueryRow(ctx, sql, workspaceID, key, viewerID).Scan(&p.id, &p.key, &p.archived)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find project: %w", err)
	}
	return &p, nil
}

func (m *model) project(ctx context.Context, workspaceID, key, viewerID string) (*project, error) {
	return findProject(ctx, m.db, workspaceID, key, viewerID, false)
}

// list returns the project's cycles, latest start first, with their progress.
func (m *model) list(ctx context.Context, projectID string) ([]Cycle, error) {
	rows, err := m.db.Query(ctx, `select `+cols+` where c.project_id = $1 group by c.id order by c.starts_on desc, c.number desc`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list cycles: %w", err)
	}
	cs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Cycle, error) { return scanCycle(row) })
	if err != nil {
		return nil, fmt.Errorf("list cycles: %w", err)
	}
	return cs, nil
}

// get returns the project's cycle with the number; nil when there's none.
func (m *model) get(ctx context.Context, projectID string, number int) (*Cycle, error) {
	return getCycle(ctx, m.db, projectID, number)
}

func getCycle(ctx context.Context, q querier, projectID string, number int) (*Cycle, error) {
	c, err := scanCycle(q.QueryRow(ctx, `select `+cols+` where c.project_id = $1 and c.number = $2 group by c.id`, projectID, number))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get cycle: %w", err)
	}
	return &c, nil
}

// writable locks the workspace's project with the key for a cycle write: errNoProject when there's none,
// errArchived when it's archived.
func writable(ctx context.Context, tx pgx.Tx, workspaceID, key, viewerID string) (*project, error) {
	p, err := findProject(ctx, tx, workspaceID, key, viewerID, true)
	switch {
	case err != nil:
		return nil, err
	case p == nil:
		return nil, errNoProject
	case p.archived:
		return nil, errArchived
	}
	return p, nil
}

// checkDates returns fieldErrors when starts–ends is too long, backwards or overlaps another of the project's cycles
// than selfID's. The caller holds the project's lock, so no concurrent write can make an overlap.
func checkDates(ctx context.Context, tx pgx.Tx, projectID, selfID string, starts, ends time.Time) error {
	if msg := checkSpan(starts, ends); msg != "" {
		return fieldErrors{"ends_on": msg}
	}
	var name string
	var s, e time.Time
	err := tx.QueryRow(ctx,
		`select coalesce(name, 'Cycle ' || number), starts_on, ends_on from cycles
		 where project_id = $1 and id is distinct from nullif($2, '')::uuid and starts_on <= $4 and ends_on >= $3 order by starts_on limit 1`,
		projectID, selfID, starts, ends).Scan(&name, &s, &e)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return fieldErrors{"starts_on": overlapMsg(name, s, e, time.Now().UTC())}
}

// create adds a cycle numbered after the project's last one and publishes e.
func (m *model) create(ctx context.Context, workspaceID, key, userID string, in input, e realtime.Event) (*Cycle, error) {
	var c *Cycle
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		p, err := writable(ctx, tx, workspaceID, key, userID)
		if err != nil {
			return err
		}
		if err := checkDates(ctx, tx, p.id, "", *in.startsOn, *in.endsOn); err != nil {
			return err
		}
		var number int
		if err := tx.QueryRow(ctx,
			`with n as (update projects set cycle_seq = cycle_seq + 1 where id = $2 returning cycle_seq)
			 insert into cycles (workspace_id, project_id, number, name, starts_on, ends_on, created_by)
			 select $1, $2, cycle_seq, $3, $4, $5, $6 from n returning number`,
			workspaceID, p.id, in.name, *in.startsOn, *in.endsOn, userID).Scan(&number); err != nil {
			return err
		}
		if c, err = getCycle(ctx, tx, p.id, number); err != nil {
			return err
		}
		return publish(ctx, tx, e, p, []int{number}, nil)
	})
	return c, wrapErr("create cycle", err)
}

// update changes the cycle's name and dates as in says and publishes e; errNoCycle when the project has no cycle
// with the number.
func (m *model) update(ctx context.Context, workspaceID, key, viewerID string, number int, in input, e realtime.Event) (*Cycle, error) {
	var c *Cycle
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		p, id, err := lockCycle(ctx, tx, workspaceID, key, viewerID, number)
		if err != nil {
			return err
		}
		var starts, ends time.Time
		if err := tx.QueryRow(ctx, `select starts_on, ends_on from cycles where id = $1`, id).Scan(&starts, &ends); err != nil {
			return err
		}
		if in.startsOn != nil {
			starts = *in.startsOn
		}
		if in.endsOn != nil {
			ends = *in.endsOn
		}
		if err := checkDates(ctx, tx, p.id, id, starts, ends); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`update cycles set name = case when $2 then $3 else name end, starts_on = $4, ends_on = $5 where id = $1`,
			id, in.setName, in.name, starts, ends); err != nil {
			return err
		}
		if c, err = getCycle(ctx, tx, p.id, number); err != nil {
			return err
		}
		ids, err := identifiers(ctx, tx, id)
		if err != nil {
			return err
		}
		return publish(ctx, tx, e, p, []int{number}, ids)
	})
	return c, wrapErr("update cycle", err)
}

// delete removes the project's cycle with the number (its issues keep no cycle) and publishes e; errNoCycle when
// there's none.
func (m *model) delete(ctx context.Context, workspaceID, key, viewerID string, number int, e realtime.Event) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		p, id, err := lockCycle(ctx, tx, workspaceID, key, viewerID, number)
		if err != nil {
			return err
		}
		ids, err := identifiers(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `delete from cycles where id = $1`, id); err != nil {
			return err
		}
		return publish(ctx, tx, e, p, []int{number}, ids)
	})
	return wrapErr("delete cycle", err)
}

// rollover moves the open issues (status not completed or canceled) of the project's cycle with the number to the
// one numbered to, logging a cycle event on each as the actor, and publishes e. Returns how many moved and the target.
// errNoCycle when the source doesn't exist; fieldErrors on "to" when the target doesn't, is the source or is past.
func (m *model) rollover(ctx context.Context, workspaceID, key string, number, to int, actorID string, e realtime.Event) (int, *Cycle, error) {
	var moved []string
	var target *Cycle
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		p, id, err := lockCycle(ctx, tx, workspaceID, key, actorID, number)
		if err != nil {
			return err
		}
		if to == number {
			return fieldErrors{"to": "Choose a different cycle."}
		}
		if target, err = getCycle(ctx, tx, p.id, to); err != nil {
			return err
		}
		switch {
		case target == nil:
			return fieldErrors{"to": "Cycle not found."}
		case target.State == "past":
			return fieldErrors{"to": "Choose an upcoming or current cycle."}
		}
		type ref struct {
			Number int    `json:"number"`
			Name   string `json:"name"`
		}
		var from ref
		if err := tx.QueryRow(ctx, `select number, coalesce(name, 'Cycle ' || number) from cycles where id = $1`, id).
			Scan(&from.Number, &from.Name); err != nil {
			return err
		}
		data, err := json.Marshal(map[string]ref{"from": from, "to": {target.Number, target.Name}})
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx,
			`with moved as (
			   update issues i set cycle_id = $2, updated_at = now() from statuses s
			   where i.cycle_id = $1 and s.id = i.status_id and s.category not in ('completed', 'canceled')
			   returning i.id, i.number),
			 logged as (insert into issue_events (issue_id, actor_id, kind, data) select id, $3, 'cycle', $4 from moved)
			 select $5::text || '-' || number from moved order by number`, id, target.ID, actorID, data, p.key)
		if err != nil {
			return err
		}
		if moved, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
			return err
		}
		if target, err = getCycle(ctx, tx, p.id, to); err != nil {
			return err
		}
		return publish(ctx, tx, e, p, []int{number, to}, moved)
	})
	return len(moved), target, wrapErr("roll over cycle", err)
}

// lockCycle locks the project for a write to its cycle with the number and returns them: errNoProject or errNoCycle
// when they don't exist, errArchived when the project is archived.
func lockCycle(ctx context.Context, tx pgx.Tx, workspaceID, key, viewerID string, number int) (*project, string, error) {
	p, err := findProject(ctx, tx, workspaceID, key, viewerID, true)
	if err != nil || p == nil {
		return nil, "", cmp(err, errNoProject)
	}
	var id string
	err = tx.QueryRow(ctx, `select id::text from cycles where project_id = $1 and number = $2`, p.id, number).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", errNoCycle
	}
	if err != nil {
		return nil, "", err
	}
	if p.archived {
		return nil, "", errArchived
	}
	return p, id, nil
}

// identifiers are the cycle's issues' identifiers.
func identifiers(ctx context.Context, tx pgx.Tx, cycleID string) ([]string, error) {
	rows, err := tx.Query(ctx,
		`select p.key || '-' || i.number from issues i join projects p on p.id = i.project_id where i.cycle_id = $1`, cycleID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// publish tells the workspace the project's cycles changed: the cycle list, the numbered cycles' pages, the project
// page and issue list, the roadmap, and the issues with the identifiers (their pages and rows show their cycle).
func publish(ctx context.Context, tx pgx.Tx, e realtime.Event, p *project, numbers []int, issues []string) error {
	e.Kind, e.Project = "cycle.changed", p.id
	key := p.key
	e.Paths = []string{realtime.Project(e.Workspace, key), realtime.ProjectIssues(e.Workspace, key), realtime.Cycles(e.Workspace, key),
		realtime.Roadmap(e.Workspace)}
	for _, n := range numbers {
		e.Paths = append(e.Paths, realtime.Cycle(e.Workspace, key, n), realtime.CycleIssues(e.Workspace, key, n))
	}
	for _, id := range issues {
		e.Paths = append(e.Paths, realtime.IssuePaths(e.Workspace, id, key)...)
	}
	return realtime.Publish(ctx, tx, e)
}

// cmp is err, or fallback when err is nil.
func cmp(err, fallback error) error {
	if err != nil {
		return err
	}
	return fallback
}

// wrapErr adds op to unexpected errors and passes the ones the controller answers through unchanged.
func wrapErr(op string, err error) error {
	if err == nil || errors.Is(err, errNoProject) || errors.Is(err, errNoCycle) || errors.Is(err, errArchived) ||
		errors.As(err, new(fieldErrors)) {
		return err
	}
	return fmt.Errorf("%s: %w", op, err)
}
