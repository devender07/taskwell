package aitest

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/doc"
	"github.com/devender07/taskwell/backend/user/internal/realtime"
	"github.com/devender07/taskwell/backend/user/internal/storage"
)

var (
	errTooMany   = errors.New("too many test cases")
	errBadOrder  = errors.New("order must list every case once")
	errNoCases   = errors.New("issue has no test cases")
	errFinished  = errors.New("run finished")
	errAIRun     = errors.New("ai run")
	errNotFound  = errors.New("not found")
	errRunActive = errors.New("a run is active on the issue")
)

const maxCases = 50

// finished are the statuses a run ends in.
var finished = []string{"passed", "failed", "error", "cancelled", "limit_reached"}

// model owns every query for test cases, runs, results, sites, accounts and usage.
type model struct{ db *pgxpool.Pool }

type Person struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type LastResult struct {
	Status string    `json:"status"`
	RunID  string    `json:"run_id"`
	Kind   string    `json:"kind"`
	At     time.Time `json:"at"`
}

type Case struct {
	ID         string      `json:"id"`
	Position   int         `json:"position"`
	Title      string      `json:"title"`
	Steps      []string    `json:"steps"`
	Expected   string      `json:"expected"`
	Source     string      `json:"source"`
	UpdatedAt  time.Time   `json:"updated_at"`
	LastResult *LastResult `json:"last_result"`
	Recorded   bool        `json:"recorded"`       // its steps are saved (by a passed AI run or the script API); runs replay them without Claude
	Playwright bool        `json:"has_playwright"` // an MCP client saved its test as Playwright code, used by the export
}

// caseFields is a case as a person or Claude wrote it.
type caseFields struct {
	Title    string   `json:"title"`
	Steps    []string `json:"steps"`
	Expected string   `json:"expected"`
}

// --- realtime ---

// scope is what a test write's realtime event needs, read in its transaction.
type scope struct{ wsID, slug, identifier, projectID string }

func issueScope(ctx context.Context, tx pgx.Tx, issueID string) (scope, error) {
	var s scope
	err := tx.QueryRow(ctx, `select w.id::text, w.slug, p.key || '-' || i.number, p.id::text
		from issues i join projects p on p.id = i.project_id join workspaces w on w.id = i.workspace_id where i.id = $1`, issueID).
		Scan(&s.wsID, &s.slug, &s.identifier, &s.projectID)
	if err != nil {
		return s, fmt.Errorf("issue scope: %w", err)
	}
	return s, nil
}

func (s scope) casesPath() string { return realtime.Issue(s.slug, s.identifier) + "/test-cases" }
func (s scope) runsPath() string  { return realtime.Issue(s.slug, s.identifier) + "/test-runs" }
func (s scope) runPath(runID string) string {
	return realtime.Workspace(s.slug) + "/test-runs/" + runID
}

// publish tells the issue's workspace (those who can see its project) kind happened, touching paths.
func publish(ctx context.Context, tx pgx.Tx, issueID, actorID, kind string, paths func(scope) []string) error {
	s, err := issueScope(ctx, tx, issueID)
	if err != nil {
		return err
	}
	return realtime.Publish(ctx, tx, realtime.Event{Kind: kind, Workspace: s.slug, Actor: actorID,
		Audience: realtime.ToWorkspace(s.wsID), Paths: paths(s), Project: s.projectID})
}

func publishCases(ctx context.Context, tx pgx.Tx, issueID, actorID string) error {
	return publish(ctx, tx, issueID, actorID, "test_cases.changed", func(s scope) []string { return []string{s.casesPath()} })
}

// publishRun publishes a change to the run, plus the issue's timeline and Home's feed when logged is set.
func publishRun(ctx context.Context, tx pgx.Tx, issueID, runID, actorID string, logged bool) error {
	return publish(ctx, tx, issueID, actorID, "test_run.changed", func(s scope) []string {
		ps := []string{s.runPath(runID), s.runsPath(), s.casesPath()} // cases show their last result
		if logged {
			ps = append(ps, realtime.Timeline(s.slug, s.identifier), realtime.Activity(s.slug))
		}
		return ps
	})
}

// --- issues ---

type issueRef struct{ id, identifier, title string }

// issue returns the workspace's issue with the project key (or an old key) and number; nil when there's none or the
// viewer can't see it.
func (m *model) issue(ctx context.Context, workspaceID, key string, number int, viewerID string) (*issueRef, error) {
	var i issueRef
	err := m.db.QueryRow(ctx, `select i.id::text, p.key || '-' || i.number, i.title
		from issues i join projects p on p.id = i.project_id
		where i.workspace_id = $1 and p.key = canonical_key($1, $2) and i.number = $3 and can_see_project(p.id, $4)`,
		workspaceID, key, number, viewerID).
		Scan(&i.id, &i.identifier, &i.title)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find issue: %w", err)
	}
	return &i, nil
}

// lockIssue serializes test writes on one issue (case counts and positions, starting runs).
func lockIssue(ctx context.Context, tx pgx.Tx, issueID string) error {
	if _, err := tx.Exec(ctx, `select 1 from issues where id = $1 for update`, issueID); err != nil {
		return fmt.Errorf("lock issue: %w", err)
	}
	return nil
}

// --- cases ---

const caseCols = `c.id::text, c.position, c.title, c.steps, c.expected, c.source, c.updated_at, c.script is not null, c.playwright is not null,
	(select json_build_object('status', r.status, 'run_id', r.run_id, 'kind', t.kind, 'at', r.marked_at)
	 from test_results r join test_runs t on t.id = r.run_id
	 where r.case_id = c.id and r.marked_at is not null order by r.marked_at desc limit 1)
	from test_cases c`

func scanCase(row pgx.Row) (Case, error) {
	var c Case
	err := row.Scan(&c.ID, &c.Position, &c.Title, &c.Steps, &c.Expected, &c.Source, &c.UpdatedAt, &c.Recorded, &c.Playwright, &c.LastResult)
	c.UpdatedAt = c.UpdatedAt.UTC()
	if c.LastResult != nil {
		c.LastResult.At = c.LastResult.At.UTC()
	}
	return c, err
}

// cases returns the issue's saved cases by position.
func (m *model) cases(ctx context.Context, issueID string) ([]Case, error) {
	rows, err := m.db.Query(ctx, `select `+caseCols+` where c.issue_id = $1 and c.deleted_at is null order by c.position, c.id`, issueID)
	if err != nil {
		return nil, fmt.Errorf("list test cases: %w", err)
	}
	cs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Case, error) { return scanCase(row) })
	if err != nil {
		return nil, fmt.Errorf("list test cases: %w", err)
	}
	return cs, nil
}

// testCase returns the workspace's saved case with the id; nil when there's none or the viewer can't see its issue.
func (m *model) testCase(ctx context.Context, workspaceID, id, viewerID string) (*Case, error) {
	c, err := scanCase(m.db.QueryRow(ctx, `select `+caseCols+` join issues i on i.id = c.issue_id
		where c.id = $1 and i.workspace_id = $2 and c.deleted_at is null and can_see_project(i.project_id, $3)`, id, workspaceID, viewerID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get test case: %w", err)
	}
	return &c, nil
}

// createCase adds the case last on the issue and returns its id; errTooMany at maxCases.
func (m *model) createCase(ctx context.Context, issueID, userID string, f caseFields) (string, error) {
	var id string
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if err := lockIssue(ctx, tx, issueID); err != nil {
			return err
		}
		var n, next int
		if err := tx.QueryRow(ctx, `select count(*), coalesce(max(position) + 1, 0) from test_cases
			where issue_id = $1 and deleted_at is null`, issueID).Scan(&n, &next); err != nil {
			return err
		}
		if n >= maxCases {
			return errTooMany
		}
		if err := tx.QueryRow(ctx, `insert into test_cases (issue_id, position, title, steps, expected, source, created_by)
			values ($1, $2, $3, $4, $5, 'manual', $6) returning id::text`, issueID, next, f.Title, f.Steps, f.Expected, userID).Scan(&id); err != nil {
			return err
		}
		return publishCases(ctx, tx, issueID, userID)
	})
	if err != nil && !errors.Is(err, errTooMany) {
		return "", fmt.Errorf("create test case: %w", err)
	}
	return id, err
}

// updateCase rewrites the workspace's case; errNotFound when there's none or the user can't see its issue.
func (m *model) updateCase(ctx context.Context, workspaceID, id, userID string, f caseFields) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var issueID string
		err := tx.QueryRow(ctx, `update test_cases c set title = $3, steps = $4, expected = $5, script = null, playwright = null, updated_at = now()
			from issues i where i.id = c.issue_id and c.id = $1 and i.workspace_id = $2 and c.deleted_at is null
			  and can_see_project(i.project_id, $6)
			returning c.issue_id::text`, id, workspaceID, f.Title, f.Steps, f.Expected, userID).Scan(&issueID)
		if errors.Is(err, pgx.ErrNoRows) {
			return errNotFound
		}
		if err != nil {
			return err
		}
		return publishCases(ctx, tx, issueID, userID)
	})
	if err != nil && !errors.Is(err, errNotFound) {
		return fmt.Errorf("update test case: %w", err)
	}
	return err
}

// deleteCase soft-deletes the workspace's case; errNotFound when there's none or the user can't see its issue.
func (m *model) deleteCase(ctx context.Context, workspaceID, id, userID string) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var issueID string
		err := tx.QueryRow(ctx, `update test_cases c set deleted_at = now()
			from issues i where i.id = c.issue_id and c.id = $1 and i.workspace_id = $2 and c.deleted_at is null
			  and can_see_project(i.project_id, $3)
			returning c.issue_id::text`, id, workspaceID, userID).Scan(&issueID)
		if errors.Is(err, pgx.ErrNoRows) {
			return errNotFound
		}
		if err != nil {
			return err
		}
		return publishCases(ctx, tx, issueID, userID)
	})
	if err != nil && !errors.Is(err, errNotFound) {
		return fmt.Errorf("delete test case: %w", err)
	}
	return err
}

// setScript saves the workspace's case's script and, when setCode, its Playwright code ("" clears it); a nil
// script clears both. errNotFound when there's no such case the user can see.
func (m *model) setScript(ctx context.Context, workspaceID, id, userID string, sc *script, setCode bool, code string) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var issueID string
		err := tx.QueryRow(ctx, `update test_cases c set script = $3,
			  playwright = case when $3::jsonb is null or $4 then nullif($5, '') else c.playwright end
			from issues i where i.id = c.issue_id and c.id = $1 and i.workspace_id = $2 and c.deleted_at is null
			  and can_see_project(i.project_id, $6)
			returning c.issue_id::text`, id, workspaceID, sc, setCode, code, userID).Scan(&issueID)
		if errors.Is(err, pgx.ErrNoRows) {
			return errNotFound
		}
		if err != nil {
			return err
		}
		return publishCases(ctx, tx, issueID, userID)
	})
	if err != nil && !errors.Is(err, errNotFound) {
		return fmt.Errorf("set test script: %w", err)
	}
	return err
}

// exportCase is a saved case with its script and Playwright code (nil when there's none), for the export.
type exportCase struct {
	caseFields
	script     *script
	playwright *string
}

// exportCases returns the issue's saved cases by position with their scripts.
func (m *model) exportCases(ctx context.Context, issueID string) ([]exportCase, error) {
	rows, err := m.db.Query(ctx, `select title, steps, expected, script, playwright from test_cases
		where issue_id = $1 and deleted_at is null order by position, id`, issueID)
	if err != nil {
		return nil, fmt.Errorf("export test cases: %w", err)
	}
	cs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (exportCase, error) {
		var c exportCase
		return c, row.Scan(&c.Title, &c.Steps, &c.Expected, &c.script, &c.playwright)
	})
	if err != nil {
		return nil, fmt.Errorf("export test cases: %w", err)
	}
	return cs, nil
}

// orderCases sets positions to the order of ids, which must be every saved case of the issue once; else errBadOrder.
func (m *model) orderCases(ctx context.Context, issueID, userID string, ids []string) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if err := lockIssue(ctx, tx, issueID); err != nil {
			return err
		}
		var ok bool
		if err := tx.QueryRow(ctx, `select coalesce(array_agg(id::text order by id::text), '{}') =
			  (select coalesce(array_agg(distinct x order by x), '{}') from unnest($2::text[]) x) and cardinality($2::text[]) = count(*)
			from test_cases where issue_id = $1 and deleted_at is null`, issueID, ids).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return errBadOrder
		}
		if _, err := tx.Exec(ctx, `update test_cases c set position = o.n - 1
			from unnest($2::uuid[]) with ordinality o(id, n) where c.id = o.id and c.issue_id = $1`, issueID, ids); err != nil {
			return err
		}
		return publishCases(ctx, tx, issueID, userID)
	})
	if err != nil && !errors.Is(err, errBadOrder) {
		return fmt.Errorf("order test cases: %w", err)
	}
	return err
}

// --- runs ---

type ResultLog struct {
	Position      int     `json:"position"`
	Action        string  `json:"action"`
	URL           *string `json:"url"`
	ScreenshotURL *string `json:"screenshot_url"`
}

type Result struct {
	ID           string      `json:"id"`
	CaseID       string      `json:"case_id"`
	Title        string      `json:"title"`
	Steps        []string    `json:"steps"`
	Expected     string      `json:"expected"`
	Status       string      `json:"status"`
	Summary      *string     `json:"summary"`
	Reproduction []string    `json:"reproduction"`
	MarkedBy     *Person     `json:"marked_by"`
	MarkedAt     *time.Time  `json:"marked_at"`
	Log          []ResultLog `json:"log"`
}

type Counts struct {
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
	Error   int `json:"error"`
	Pending int `json:"pending"` // pending or running
}

type IssueRef struct {
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
}

// Run is a run summary (GET .../test-runs); GET /test-runs/{id} adds results and the latest screenshot.
type Run struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	Issue      IssueRef   `json:"issue"`
	Status     string     `json:"status"`
	Error      *string    `json:"error"`
	StartedBy  *Person    `json:"started_by"`
	CreatedAt  time.Time  `json:"created_at"`
	FinishedAt *time.Time `json:"finished_at"`
	CostUSD    *float64   `json:"cost_usd"`
	Counts     Counts     `json:"counts"`

	issueID string
}

type RunDetail struct {
	*Run
	Results             []Result `json:"results"`
	LatestScreenshotURL *string  `json:"latest_screenshot_url"`
}

const runCols = `r.id::text, r.kind, p.key || '-' || i.number, i.title, r.status, r.error,
	(select json_build_object('id', u.id, 'name', u.name) from users u where u.id = r.started_by),
	r.created_at, r.finished_at, r.cost_micros, r.issue_id::text,
	count(x.id) filter (where x.status = 'passed'), count(x.id) filter (where x.status = 'failed'),
	count(x.id) filter (where x.status = 'skipped'), count(x.id) filter (where x.status = 'error'),
	count(x.id) filter (where x.status in ('pending', 'running'))
	from test_runs r join issues i on i.id = r.issue_id join projects p on p.id = i.project_id
	left join test_results x on x.run_id = r.id`

func scanRun(row pgx.Row) (Run, error) {
	var r Run
	var micros int64
	err := row.Scan(&r.ID, &r.Kind, &r.Issue.Identifier, &r.Issue.Title, &r.Status, &r.Error, &r.StartedBy, &r.CreatedAt,
		&r.FinishedAt, &micros, &r.issueID, &r.Counts.Passed, &r.Counts.Failed, &r.Counts.Skipped, &r.Counts.Error, &r.Counts.Pending)
	if r.Kind == "ai" {
		usd := float64(micros) / 1e6
		r.CostUSD = &usd
	}
	r.CreatedAt, r.FinishedAt = r.CreatedAt.UTC(), utc(r.FinishedAt)
	return r, err
}

// runsPage caps a page of an issue's test history.
const runsPage = 10

// runs returns the issue's newest runsPage runs created before before (all when nil), and whether older ones exist.
func (m *model) runs(ctx context.Context, issueID string, before *time.Time) ([]Run, bool, error) {
	rows, err := m.db.Query(ctx, `select `+runCols+` where r.issue_id = $1 and ($2::timestamptz is null or r.created_at < $2)
		group by r.id, i.id, i.number, i.title, p.id order by r.created_at desc, r.id desc limit $3`, issueID, before, runsPage+1)
	if err != nil {
		return nil, false, fmt.Errorf("list test runs: %w", err)
	}
	rs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Run, error) { return scanRun(row) })
	if err != nil {
		return nil, false, fmt.Errorf("list test runs: %w", err)
	}
	if len(rs) > runsPage {
		return rs[:runsPage], true, nil
	}
	return rs, false, nil
}

// run returns the workspace's run with the id and its results; nil when there's none or the viewer can't see its issue.
func (m *model) run(ctx context.Context, workspaceID, id, viewerID string) (*RunDetail, error) {
	r, err := scanRun(m.db.QueryRow(ctx, `select `+runCols+` where r.id = $1 and r.workspace_id = $2 and can_see_project(p.id, $3)
		group by r.id, i.id, i.number, i.title, p.id`, id, workspaceID, viewerID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get test run: %w", err)
	}
	d := &RunDetail{Run: &r}
	rows, err := m.db.Query(ctx, `select x.id::text, x.case_id::text, x.title, x.steps, x.expected, x.status, x.summary,
		coalesce(x.reproduction, '{}'), (select json_build_object('id', u.id, 'name', u.name) from users u where u.id = x.marked_by),
		x.marked_at from test_results x where x.run_id = $1 order by x.position, x.id`, id)
	if err != nil {
		return nil, fmt.Errorf("get test results: %w", err)
	}
	d.Results, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Result, error) {
		x := Result{Log: []ResultLog{}}
		err := row.Scan(&x.ID, &x.CaseID, &x.Title, &x.Steps, &x.Expected, &x.Status, &x.Summary, &x.Reproduction, &x.MarkedBy, &x.MarkedAt)
		x.MarkedAt = utc(x.MarkedAt)
		return x, err
	})
	if err != nil {
		return nil, fmt.Errorf("get test results: %w", err)
	}
	if r.Kind != "manual" {
		if err := m.addLogs(ctx, d); err != nil {
			return nil, err
		}
	}
	return d, nil
}

// addLogs fills the AI or replay run's step logs and latest screenshot.
func (m *model) addLogs(ctx context.Context, d *RunDetail) error {
	rows, err := m.db.Query(ctx, `select s.result_id::text, s.position, s.action, s.url,
		  case when f.id is not null then '/api/files/' || f.id || '/' || f.name end
		from test_steps s join test_results x on x.id = s.result_id left join files f on f.id = s.screenshot_id
		where x.run_id = $1 order by s.created_at, s.id`, d.ID)
	if err != nil {
		return fmt.Errorf("get test steps: %w", err)
	}
	byResult := map[string]*Result{}
	for i := range d.Results {
		byResult[d.Results[i].ID] = &d.Results[i]
	}
	var resultID string
	var l ResultLog
	_, err = pgx.ForEachRow(rows, []any{&resultID, &l.Position, &l.Action, &l.URL, &l.ScreenshotURL}, func() error {
		if x := byResult[resultID]; x != nil {
			x.Log = append(x.Log, l)
		}
		if l.ScreenshotURL != nil {
			d.LatestScreenshotURL = l.ScreenshotURL
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("get test steps: %w", err)
	}
	return nil
}

// runKind returns the workspace's run's kind; "" when there's none or the viewer can't see its issue.
func (m *model) runKind(ctx context.Context, workspaceID, id, viewerID string) (string, error) {
	var kind string
	err := m.db.QueryRow(ctx, `select kind from test_runs where id = $1 and workspace_id = $2 and `+runSeen,
		id, workspaceID, viewerID).Scan(&kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get test run: %w", err)
	}
	return kind, nil
}

// createManual starts a manual run with a pending result per saved case and returns its id; errNoCases without any.
func (m *model) createManual(ctx context.Context, workspaceID, issueID, userID string) (string, error) {
	var id string
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if err := lockIssue(ctx, tx, issueID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `insert into test_runs (workspace_id, issue_id, kind, started_by, status)
			values ($1, $2, 'manual', $3, 'running') returning id::text`, workspaceID, issueID, userID).Scan(&id); err != nil {
			return err
		}
		n, err := snapshotCases(ctx, tx, id, issueID, "", maxCases)
		if err != nil {
			return err
		}
		if n == 0 {
			return errNoCases
		}
		return publishRun(ctx, tx, issueID, id, userID, false)
	})
	if err != nil && !errors.Is(err, errNoCases) {
		return "", fmt.Errorf("start manual run: %w", err)
	}
	return id, err
}

// snapshotCases copies the issue's first limit saved cases into the run as pending results and returns how many.
func snapshotCases(ctx context.Context, tx pgx.Tx, runID, issueID, caseID string, limit int) (int64, error) {
	tag, err := tx.Exec(ctx, `insert into test_results (run_id, case_id, position, title, steps, expected)
		select $1, id, row_number() over (order by position, id) - 1, title, steps, expected
		from test_cases where issue_id = $2 and deleted_at is null and ($4 = '' or id = nullif($4, '')::uuid)
		order by position, id limit $3`, runID, issueID, limit, caseID)
	if err != nil {
		return 0, fmt.Errorf("snapshot test cases: %w", err)
	}
	return tag.RowsAffected(), nil
}

// runSeen is whether the viewer ($3) can see the issue of the run in test_runs.
const runSeen = `can_see_project((select project_id from issues where id = test_runs.issue_id), $3)`

// lockRun locks the workspace's run and returns its kind, status and issue; errNotFound when there's none or the
// viewer can't see its issue.
func lockRun(ctx context.Context, tx pgx.Tx, workspaceID, runID, viewerID string) (kind, status, issueID string, err error) {
	err = tx.QueryRow(ctx, `select kind, status, issue_id::text from test_runs where id = $1 and workspace_id = $2 and `+runSeen+`
		for update`, runID, workspaceID, viewerID).Scan(&kind, &status, &issueID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = errNotFound
	}
	return kind, status, issueID, err
}

// mark records a manual result; when it was the last unmarked one the run finishes, failed if any case failed.
// errNotFound for a missing run or result, errAIRun or errFinished when it can't be marked.
func (m *model) mark(ctx context.Context, workspaceID, runID, resultID, userID, status string, note *string) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		kind, runStatus, issueID, err := lockRun(ctx, tx, workspaceID, runID, userID)
		switch {
		case err != nil:
			return err
		case kind != "manual":
			return errAIRun
		case runStatus != "running":
			return errFinished
		}
		tag, err := tx.Exec(ctx, `update test_results set status = $3, summary = $4, marked_by = $5, marked_at = now()
			where id = $2 and run_id = $1`, runID, resultID, status, note, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errNotFound
		}
		var done, failed bool
		if err := tx.QueryRow(ctx, `select not bool_or(status = 'pending'), bool_or(status = 'failed') from test_results where run_id = $1`,
			runID).Scan(&done, &failed); err != nil {
			return err
		}
		if !done {
			return publishRun(ctx, tx, issueID, runID, userID, false)
		}
		end := "passed"
		if failed {
			end = "failed"
		}
		return finishRun(ctx, tx, runID, userID, end, nil)
	})
	if err != nil && !errors.Is(err, errNotFound) && !errors.Is(err, errAIRun) && !errors.Is(err, errFinished) {
		return fmt.Errorf("mark test result: %w", err)
	}
	return err
}

// finishRun ends the run with status (and the user-facing message, if any), logs it on the issue as its starter's
// unless it was cancelled, and publishes it as actorID's change.
func finishRun(ctx context.Context, tx pgx.Tx, runID, actorID, status string, msg *string) error {
	var issueID, kind string
	var startedBy *string
	var c Counts
	err := tx.QueryRow(ctx, `update test_runs r set status = $2, error = $3, finished_at = now(), cancel_requested = false
		where id = $1 returning issue_id::text, kind, started_by::text,
		  (select count(*) filter (where status = 'passed') from test_results where run_id = r.id),
		  (select count(*) filter (where status = 'failed') from test_results where run_id = r.id),
		  (select count(*) filter (where status = 'skipped') from test_results where run_id = r.id),
		  (select count(*) filter (where status = 'error') from test_results where run_id = r.id)`, runID, status, msg).
		Scan(&issueID, &kind, &startedBy, &c.Passed, &c.Failed, &c.Skipped, &c.Error)
	if err != nil {
		return fmt.Errorf("finish run: %w", err)
	}
	logged := status != "cancelled"
	if logged {
		data, err := json.Marshal(map[string]any{"run_id": runID, "kind": kind, "status": status,
			"passed": c.Passed, "failed": c.Failed, "skipped": c.Skipped, "error": c.Error})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `insert into issue_events (issue_id, actor_id, kind, data) values ($1, $2, 'test_run', $3)`,
			issueID, startedBy, data); err != nil {
			return fmt.Errorf("log run: %w", err)
		}
	}
	return publishRun(ctx, tx, issueID, runID, actorID, logged)
}

// cancel ends a manual or queued run, or asks the tester to stop an AI run in progress; errNotFound or errFinished.
func (m *model) cancel(ctx context.Context, workspaceID, runID, userID string) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		kind, status, issueID, err := lockRun(ctx, tx, workspaceID, runID, userID)
		switch {
		case err != nil:
			return err
		case isFinished(status):
			return errFinished
		case kind == "manual" || status == "queued":
			return finishRun(ctx, tx, runID, userID, "cancelled", nil)
		}
		if _, err := tx.Exec(ctx, `update test_runs set cancel_requested = true where id = $1`, runID); err != nil {
			return err
		}
		return publishRun(ctx, tx, issueID, runID, userID, false)
	})
	if err != nil && !errors.Is(err, errNotFound) && !errors.Is(err, errFinished) {
		return fmt.Errorf("cancel test run: %w", err)
	}
	return err
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// --- AI testing: sites, accounts, usage and the tester ---

var (
	errTaken        = errors.New("already exists")
	errNoSites      = errors.New("no test sites")
	errNoScript     = errors.New("no recorded test cases")
	errLimit        = errors.New("monthly limit reached")
	errSiteNotFound = errors.New("test site not found")
)

const (
	heartbeatKey   = "ai_tester_heartbeat"
	heartbeatFresh = 90 * time.Second
	msgLimit       = "This month's AI test limit is reached."
)

type Site struct {
	ID      string `json:"id"`
	BaseURL string `json:"base_url"`
	Host    string `json:"host"`
}

type Account struct {
	ID          string  `json:"id"`
	Label       string  `json:"label"`
	Username    string  `json:"username"`
	PasswordSet bool    `json:"password_set"`
	SiteID      *string `json:"site_id"` // nil: any of the workspace's sites
}

func isUnique(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}

// publishWS tells the workspace the list at path (under the workspace) changed.
func publishWS(ctx context.Context, tx pgx.Tx, wsID, slug, actorID, kind, path string) error {
	return realtime.Publish(ctx, tx, realtime.Event{Kind: kind, Workspace: slug, Actor: actorID,
		Audience: realtime.ToWorkspace(wsID), Paths: []string{realtime.Workspace(slug) + path}})
}

// --- sites ---

func (m *model) sites(ctx context.Context, wsID string) ([]Site, error) {
	rows, err := m.db.Query(ctx, `select id::text, base_url, host from test_sites where workspace_id = $1 order by created_at, id`, wsID)
	if err != nil {
		return nil, fmt.Errorf("list test sites: %w", err)
	}
	ss, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Site])
	if err != nil {
		return nil, fmt.Errorf("list test sites: %w", err)
	}
	return ss, nil
}

// createSite adds the site; errTaken when the workspace has its host already.
func (m *model) createSite(ctx context.Context, wsID, slug, actorID, base, host string) (Site, error) {
	s := Site{BaseURL: base, Host: host}
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `insert into test_sites (workspace_id, base_url, host) values ($1, $2, $3) returning id::text`,
			wsID, base, host).Scan(&s.ID); err != nil {
			return err
		}
		return publishWS(ctx, tx, wsID, slug, actorID, "test_sites.changed", "/test-sites")
	})
	if isUnique(err) {
		return s, errTaken
	}
	if err != nil {
		return s, fmt.Errorf("create test site: %w", err)
	}
	return s, nil
}

// deleteSite removes the workspace's site; errNotFound when there's none.
func (m *model) deleteSite(ctx context.Context, wsID, slug, actorID, id string) error {
	return m.deleteRow(ctx, `delete from test_sites where id = $1 and workspace_id = $2`, wsID, slug, actorID, id,
		"test_sites.changed", "/test-sites")
}

func (m *model) deleteRow(ctx context.Context, sql, wsID, slug, actorID, id, kind, path string) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, sql, id, wsID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errNotFound
		}
		return publishWS(ctx, tx, wsID, slug, actorID, kind, path)
	})
	if err != nil && !errors.Is(err, errNotFound) {
		return fmt.Errorf("delete %s: %w", path, err)
	}
	return err
}

// --- accounts ---

func (m *model) accounts(ctx context.Context, wsID string) ([]Account, error) {
	rows, err := m.db.Query(ctx, `select id::text, label, username, true, site_id::text from test_accounts where workspace_id = $1
		order by lower(label), id`, wsID)
	if err != nil {
		return nil, fmt.Errorf("list test accounts: %w", err)
	}
	as, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Account])
	if err != nil {
		return nil, fmt.Errorf("list test accounts: %w", err)
	}
	return as, nil
}

// saveAccount creates the account (id "") or updates the workspace's account with the id, sealing a non-empty
// password (empty keeps the stored one) and tying it to the site with siteID ("" for any site). errTaken for a
// duplicate label, errNotFound for a missing account, errSiteNotFound for a site not in the workspace.
func (m *model) saveAccount(ctx context.Context, key []byte, wsID, slug, actorID, id, label, username, password, siteID string) (Account, error) {
	a := Account{ID: id, Label: label, Username: username, PasswordSet: true}
	if siteID != "" {
		a.SiteID = &siteID
	}
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if siteID != "" {
			var ok bool
			if err := tx.QueryRow(ctx, `select exists(select 1 from test_sites where id = $1 and workspace_id = $2)`, siteID, wsID).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return errSiteNotFound
			}
		}
		if id == "" {
			if err := tx.QueryRow(ctx, `select uuidv7()::text`).Scan(&a.ID); err != nil {
				return err
			}
		}
		var sealed *string
		if password != "" {
			s, err := seal(key, wsID, a.ID, password)
			if err != nil {
				return err
			}
			sealed = &s
		}
		var tag pgconn.CommandTag
		var err error
		if id == "" {
			tag, err = tx.Exec(ctx, `insert into test_accounts (id, workspace_id, label, username, password, site_id) values ($1, $2, $3, $4, $5, $6)`,
				a.ID, wsID, label, username, sealed, a.SiteID)
		} else {
			tag, err = tx.Exec(ctx, `update test_accounts set label = $3, username = $4, password = coalesce($5, password), site_id = $6
				where id = $1 and workspace_id = $2`, a.ID, wsID, label, username, sealed, a.SiteID)
		}
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errNotFound
		}
		return publishWS(ctx, tx, wsID, slug, actorID, "test_accounts.changed", "/test-accounts")
	})
	switch {
	case isUnique(err):
		return a, errTaken
	case err != nil && !errors.Is(err, errNotFound) && !errors.Is(err, errSiteNotFound):
		return a, fmt.Errorf("save test account: %w", err)
	}
	return a, err
}

func (m *model) deleteAccount(ctx context.Context, wsID, slug, actorID, id string) error {
	return m.deleteRow(ctx, `delete from test_accounts where id = $1 and workspace_id = $2`, wsID, slug, actorID, id,
		"test_accounts.changed", "/test-accounts")
}

// password returns the workspace's account's sealed password; "" when there's none.
func (m *model) password(ctx context.Context, wsID, accountID string) (string, error) {
	var p string
	err := m.db.QueryRow(ctx, `select password from test_accounts where id = $1 and workspace_id = $2`, accountID, wsID).Scan(&p)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("read test account: %w", err)
	}
	return p, nil
}

// --- usage ---

// monthStart is the start of t's UTC month, when monthly limits reset.
func monthStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// spent is the workspace's Claude spend this UTC month in micro-dollars.
func spent(ctx context.Context, q querier, wsID string) (int64, error) {
	var n int64
	if err := q.QueryRow(ctx, `select coalesce(sum(cost_micros), 0)::bigint from ai_usage where workspace_id = $1 and created_at >= $2`,
		wsID, monthStart(time.Now())).Scan(&n); err != nil {
		return 0, fmt.Errorf("sum ai usage: %w", err)
	}
	return n, nil
}

// testerURL is the internal URL of the tester that checked in within heartbeatFresh; "" when none did.
func (m *model) testerURL(ctx context.Context) (string, error) {
	var u string
	err := m.db.QueryRow(ctx, `select value from system_config where key = $1 and updated_at > now() - make_interval(secs => $2)`,
		heartbeatKey, heartbeatFresh.Seconds()).Scan(&u)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("read tester heartbeat: %w", err)
	}
	return u, nil
}

// createAI queues an AI run and returns its id: errNoSites, errLimit (spend >= limitMicros) or errRunActive when
// one is already queued or running on the issue.
func (m *model) createAI(ctx context.Context, wsID, issueID, userID, caseID string, regenerate bool, limitMicros int64) (string, error) {
	var id string
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if err := lockIssue(ctx, tx, issueID); err != nil {
			return err
		}
		var sites int
		var active bool
		if err := tx.QueryRow(ctx, `select (select count(*) from test_sites where workspace_id = $1),
			exists (select 1 from test_runs where issue_id = $2 and kind in ('ai', 'replay') and status in ('queued', 'generating', 'running'))`,
			wsID, issueID).Scan(&sites, &active); err != nil {
			return err
		}
		if sites == 0 {
			return errNoSites
		}
		if caseID != "" {
			var ok bool
			if err := tx.QueryRow(ctx, `select exists (select 1 from test_cases where id = $1 and issue_id = $2 and deleted_at is null)`,
				caseID, issueID).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return errNotFound
			}
		}
		n, err := spent(ctx, tx, wsID)
		if err != nil {
			return err
		}
		switch {
		case n >= limitMicros:
			return errLimit
		case active:
			return errRunActive
		}
		if err := tx.QueryRow(ctx, `insert into test_runs (workspace_id, issue_id, kind, regenerate, started_by, status, proxy_token, case_id)
			values ($1, $2, 'ai', $3, $4, 'queued', $5, $6) returning id::text`, wsID, issueID, regenerate, userID, rand.Text(), nullable(caseID)).Scan(&id); err != nil {
			return err
		}
		return publishRun(ctx, tx, issueID, id, userID, false)
	})
	if err != nil && !errors.Is(err, errNoSites) && !errors.Is(err, errLimit) && !errors.Is(err, errRunActive) && !errors.Is(err, errNotFound) {
		return "", fmt.Errorf("queue ai run: %w", err)
	}
	return id, err
}

// createReplay queues a replay run and returns its id: errNoSites, errNoScript when no case is recorded, or
// errRunActive when an AI or replay run is already queued or running on the issue. It never checks the spend limit.
func (m *model) createReplay(ctx context.Context, wsID, issueID, userID string) (string, error) {
	var id string
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if err := lockIssue(ctx, tx, issueID); err != nil {
			return err
		}
		var sites int
		var recorded, active bool
		if err := tx.QueryRow(ctx, `select (select count(*) from test_sites where workspace_id = $1),
			exists (select 1 from test_cases where issue_id = $2 and deleted_at is null and script is not null),
			exists (select 1 from test_runs where issue_id = $2 and kind in ('ai', 'replay') and status in ('queued', 'generating', 'running'))`,
			wsID, issueID).Scan(&sites, &recorded, &active); err != nil {
			return err
		}
		switch {
		case sites == 0:
			return errNoSites
		case !recorded:
			return errNoScript
		case active:
			return errRunActive
		}
		if err := tx.QueryRow(ctx, `insert into test_runs (workspace_id, issue_id, kind, started_by, status, proxy_token)
			values ($1, $2, 'replay', $3, 'queued', $4) returning id::text`, wsID, issueID, userID, rand.Text()).Scan(&id); err != nil {
			return err
		}
		return publishRun(ctx, tx, issueID, id, userID, false)
	})
	if err != nil && !errors.Is(err, errNoSites) && !errors.Is(err, errNoScript) && !errors.Is(err, errRunActive) {
		return "", fmt.Errorf("queue replay run: %w", err)
	}
	return id, err
}

// --- tester ---

// heartbeat records that the tester at testerURL is up; the API sends new browser sessions there.
// ponytail: the last tester to beat gets every new session; spread them by load if there are several testers.
func (m *model) heartbeat(ctx context.Context, testerURL string) error {
	_, err := m.db.Exec(ctx, `insert into system_config (key, value) values ($1, $2)
		on conflict (key) do update set value = excluded.value, updated_at = now()`, heartbeatKey, testerURL)
	if err != nil {
		return fmt.Errorf("tester heartbeat: %w", err)
	}
	return nil
}

const (
	staleAfter = 15 * time.Minute
	msgStale   = "The test runner stopped. Run it again."
)

// failStale ends AI and replay runs a crashed tester left generating or running, claimed over staleAfter ago.
func (m *model) failStale(ctx context.Context) error {
	return pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select id::text from test_runs where kind in ('ai', 'replay') and status in ('generating', 'running')
			and claimed_at < now() - make_interval(secs => $1) for update skip locked`, staleAfter.Seconds())
		if err != nil {
			return fmt.Errorf("find stale runs: %w", err)
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return fmt.Errorf("find stale runs: %w", err)
		}
		for _, id := range ids {
			if err := endAI(ctx, tx, id, "error", msgStale); err != nil {
				return err
			}
		}
		return nil
	})
}

// claimLock serializes claims across testers so the per-workspace cap holds.
// ponytail: one global advisory lock per claim; per-workspace locks if claims ever queue behind it.
const claimLock = 0x7465737472756e // "testrun"

// claim takes the oldest queued AI or replay run whose workspace has fewer than workspaceCap of them running (open
// browser sessions count too), marks
// it generating (an AI run that needs cases) or running, and returns its id; "" when there's none.
func (m *model) claim(ctx context.Context, workspaceCap int, testerURL string) (string, error) {
	var id string
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, claimLock); err != nil {
			return err
		}
		var issueID string
		err := tx.QueryRow(ctx, `select r.id::text, r.issue_id::text from test_runs r
			where r.kind in ('ai', 'replay') and r.status = 'queued'
			  and (select count(*) from test_runs o where o.workspace_id = r.workspace_id and o.kind in ('ai', 'replay')
			       and o.status in ('generating', 'running'))
			    + (select count(*) from browser_sessions b where b.workspace_id = r.workspace_id and b.closed_at is null
			       and b.created_at > now() - make_interval(secs => $2)) < $1
			order by r.created_at, r.id limit 1 for update skip locked`, workspaceCap, sessionMax.Seconds()).Scan(&id, &issueID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `update test_runs r set claimed_at = now(), tester_url = $2,
			status = case when r.kind = 'ai' and (r.regenerate or not exists (select 1 from test_cases c where c.issue_id = r.issue_id and c.deleted_at is null))
			         then 'generating' else 'running' end
			where id = $1`, id, testerURL); err != nil {
			return err
		}
		return publishRun(ctx, tx, issueID, id, "", false)
	})
	if err != nil {
		return "", fmt.Errorf("claim ai run: %w", err)
	}
	return id, nil
}

// liveTester returns the tester URL of the workspace's AI or replay run while it's generating or running and the
// viewer can see its issue, else "".
func (m *model) liveTester(ctx context.Context, workspaceID, id, viewerID string) (string, error) {
	var u string
	err := m.db.QueryRow(ctx, `select coalesce(tester_url, '') from test_runs
		where id = $1 and workspace_id = $2 and kind in ('ai', 'replay') and status in ('generating', 'running') and `+runSeen,
		id, workspaceID, viewerID).Scan(&u)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return u, err
}

type accountRef struct{ id, label, username, host string } // host "": any of the workspace's sites

// job is what the tester needs to run a claimed run.
type job struct {
	runID, wsID, issueID, kind, status, token, startedBy, caseID string
	regenerate                                                   bool
	issueTitle, description, project                             string
	sites                                                        []Site
	accounts                                                     []accountRef
}

func (m *model) job(ctx context.Context, runID string) (*job, error) {
	j := job{runID: runID}
	var desc []byte
	var startedBy *string
	err := m.db.QueryRow(ctx, `select r.workspace_id::text, r.issue_id::text, r.kind, r.status, r.proxy_token, r.regenerate, r.started_by::text,
		  coalesce(r.case_id::text, ''), i.title, i.description, p.name
		from test_runs r join issues i on i.id = r.issue_id join projects p on p.id = i.project_id where r.id = $1`, runID).
		Scan(&j.wsID, &j.issueID, &j.kind, &j.status, &j.token, &j.regenerate, &startedBy, &j.caseID, &j.issueTitle, &desc, &j.project)
	if err != nil {
		return nil, fmt.Errorf("read ai run: %w", err)
	}
	if startedBy != nil {
		j.startedBy = *startedBy
	}
	j.description = doc.PlainText(desc)
	if j.sites, err = m.sites(ctx, j.wsID); err != nil {
		return nil, err
	}
	if j.accounts, err = m.accountRefs(ctx, j.wsID); err != nil {
		return nil, err
	}
	return &j, nil
}

// accountRefs lists the workspace's test accounts with the host each may be typed on.
func (m *model) accountRefs(ctx context.Context, wsID string) ([]accountRef, error) {
	rows, err := m.db.Query(ctx, `select a.id::text, a.label, a.username, coalesce(s.host, '') from test_accounts a
		left join test_sites s on s.id = a.site_id where a.workspace_id = $1 order by lower(a.label), a.id`, wsID)
	if err != nil {
		return nil, fmt.Errorf("read test accounts: %w", err)
	}
	as, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (accountRef, error) {
		var a accountRef
		return a, row.Scan(&a.id, &a.label, &a.username, &a.host)
	})
	if err != nil {
		return nil, fmt.Errorf("read test accounts: %w", err)
	}
	return as, nil
}

const (
	maxAIRunCases = 20
	msgOverLimit  = "Over the 20-case limit for AI runs"
)

// startCases saves Claude's cases on the issue when generated isn't nil (replacing the old ones, logged when the
// run regenerates), then sets the run running with a pending result per case, an AI run's past maxAIRunCases skipped.
func (m *model) startCases(ctx context.Context, j *job, generated []caseFields) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if err := lockIssue(ctx, tx, j.issueID); err != nil {
			return err
		}
		if generated != nil {
			if _, err := tx.Exec(ctx, `update test_cases set deleted_at = now() where issue_id = $1 and deleted_at is null`, j.issueID); err != nil {
				return err
			}
			for i, c := range generated {
				if _, err := tx.Exec(ctx, `insert into test_cases (issue_id, position, title, steps, expected, source, created_by)
					values ($1, $2, $3, $4, $5, 'ai', $6)`, j.issueID, i, c.Title, c.Steps, c.Expected, nullable(j.startedBy)); err != nil {
					return err
				}
			}
			if j.regenerate {
				if _, err := tx.Exec(ctx, `insert into issue_events (issue_id, actor_id, kind, data)
					values ($1, $2, 'test_cases_regenerated', jsonb_build_object('run_id', $3::text))`, j.issueID, nullable(j.startedBy), j.runID); err != nil {
					return err
				}
			}
			if err := publishCases(ctx, tx, j.issueID, j.startedBy); err != nil {
				return err
			}
		}
		if _, err := snapshotCases(ctx, tx, j.runID, j.issueID, j.caseID, maxCases); err != nil {
			return err
		}
		if j.kind == "ai" {
			if _, err := tx.Exec(ctx, `update test_results set status = 'skipped', summary = $3, marked_at = now()
				where run_id = $1 and position >= $2`, j.runID, maxAIRunCases, msgOverLimit); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `update test_runs set status = 'running' where id = $1`, j.runID); err != nil {
			return err
		}
		return publishRun(ctx, tx, j.issueID, j.runID, "", generated != nil && j.regenerate)
	})
	if err != nil {
		return fmt.Errorf("start ai cases: %w", err)
	}
	return nil
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// script returns the case's recorded steps; nil when it has none.
func (m *model) script(ctx context.Context, caseID string) (*script, error) {
	var sc *script
	err := m.db.QueryRow(ctx, `select script from test_cases where id = $1 and deleted_at is null`, caseID).Scan(&sc)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read test script: %w", err)
	}
	return sc, nil
}

// saveScript records a passed case's steps for replay.
func (m *model) saveScript(ctx context.Context, caseID string, sc *script) error {
	if _, err := m.db.Exec(ctx, `update test_cases set script = $2 where id = $1 and deleted_at is null`, caseID, sc); err != nil {
		return fmt.Errorf("save test script: %w", err)
	}
	return nil
}

// pending returns the run's results still to run, in order.
func (m *model) pending(ctx context.Context, runID string) ([]Result, error) {
	rows, err := m.db.Query(ctx, `select id::text, case_id::text, title, steps, expected from test_results
		where run_id = $1 and status = 'pending' order by position, id`, runID)
	if err != nil {
		return nil, fmt.Errorf("pending results: %w", err)
	}
	rs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Result, error) {
		var x Result
		return x, row.Scan(&x.ID, &x.CaseID, &x.Title, &x.Steps, &x.Expected)
	})
	if err != nil {
		return nil, fmt.Errorf("pending results: %w", err)
	}
	return rs, nil
}

// setResult records an AI result's status (running, or how it ended) and publishes the run.
func (m *model) setResult(ctx context.Context, j *job, resultID, status string, summary *string, repro []string) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `update test_results set status = $2, summary = $3, reproduction = $4,
			marked_at = case when $2 <> 'running' then now() end where id = $1`, resultID, status, summary, repro); err != nil {
			return err
		}
		return publishRun(ctx, tx, j.issueID, j.runID, "", false)
	})
	if err != nil {
		return fmt.Errorf("set ai result: %w", err)
	}
	return nil
}

// addStep logs a step with its screenshot (JPEG bytes, or nil): the file's row and bytes, the step and the
// run's event commit together.
func (m *model) addStep(ctx context.Context, j *job, files *storage.Store, resultID string, position int, action, pageURL string, jpeg []byte) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var fileID *string
		if jpeg != nil {
			var id string
			if err := tx.QueryRow(ctx, `insert into files (workspace_id, name, content_type, size) values ($1, $2, 'image/jpeg', $3)
				returning id::text`, j.wsID, fmt.Sprintf("step-%d.jpg", position+1), len(jpeg)).Scan(&id); err != nil {
				return err
			}
			if err := files.Put(ctx, id, "image/jpeg", "inline", bytes.NewReader(jpeg)); err != nil {
				return err
			}
			fileID = &id
		}
		if _, err := tx.Exec(ctx, `insert into test_steps (result_id, position, action, url, screenshot_id) values ($1, $2, $3, $4, $5)`,
			resultID, position, action, nullable(pageURL), fileID); err != nil {
			return err
		}
		return publishRun(ctx, tx, j.issueID, j.runID, "", false)
	})
	if err != nil {
		return fmt.Errorf("add test step: %w", err)
	}
	return nil
}

// addUsage adds a Claude call's tokens and cost to the run and the workspace's spend.
func (m *model) addUsage(ctx context.Context, j *job, u usage) error {
	cost := u.costMicros()
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `update test_runs set input_tokens = input_tokens + $2, output_tokens = output_tokens + $3,
			cache_read_tokens = cache_read_tokens + $4, cache_write_tokens = cache_write_tokens + $5, cost_micros = cost_micros + $6
			where id = $1`, j.runID, u.input, u.output, u.cacheRead, u.cacheWrite, cost); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `insert into ai_usage (workspace_id, run_id, cost_micros) values ($1, $2, $3)`, j.wsID, j.runID, cost)
		return err
	})
	if err != nil {
		return fmt.Errorf("add ai usage: %w", err)
	}
	return nil
}

// cancelRequested reports whether someone asked to stop the run.
func (m *model) cancelRequested(ctx context.Context, runID string) (bool, error) {
	var c bool
	if err := m.db.QueryRow(ctx, `select cancel_requested from test_runs where id = $1`, runID).Scan(&c); err != nil {
		return false, fmt.Errorf("read cancel: %w", err)
	}
	return c, nil
}

// finishAI ends the AI run (unless it already ended): its unfinished results are skipped, then it gets status and
// the user-facing msg ("" for none).
func (m *model) finishAI(ctx context.Context, runID, status, msg string) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var cur string
		if err := tx.QueryRow(ctx, `select status from test_runs where id = $1 for update`, runID).Scan(&cur); err != nil {
			return err
		}
		if isFinished(cur) {
			return nil
		}
		return endAI(ctx, tx, runID, status, msg)
	})
	if err != nil {
		return fmt.Errorf("finish ai run: %w", err)
	}
	return nil
}

func endAI(ctx context.Context, tx pgx.Tx, runID, status, msg string) error {
	if _, err := tx.Exec(ctx, `update test_results set status = 'skipped', marked_at = now(), summary = coalesce(summary, 'Not run')
		where run_id = $1 and status in ('pending', 'running')`, runID); err != nil {
		return err
	}
	return finishRun(ctx, tx, runID, "", status, nullable(msg))
}

// aiOutcome is how a run whose cases all ran ends: failed if any failed, error if any couldn't finish, else passed.
func (m *model) aiOutcome(ctx context.Context, runID string) (status, msg string, err error) {
	var failed, errored bool
	if err := m.db.QueryRow(ctx, `select coalesce(bool_or(status = 'failed'), false), coalesce(bool_or(status = 'error'), false)
		from test_results where run_id = $1`, runID).Scan(&failed, &errored); err != nil {
		return "", "", fmt.Errorf("ai outcome: %w", err)
	}
	switch {
	case failed:
		return "failed", "", nil
	case errored:
		return "error", "Some test cases couldn't finish.", nil
	}
	return "passed", "", nil
}

// --- browser sessions ---

// sessionRoom reports whether the workspace has fewer than workspaceCap AI or replay runs running and browser
// sessions open together.
// ponytail: soft cap, two sessions opened at the same moment may both pass; lock like claim if it matters.
func (m *model) sessionRoom(ctx context.Context, wsID string, workspaceCap int) (bool, error) {
	var ok bool
	err := m.db.QueryRow(ctx, `select (select count(*) from test_runs where workspace_id = $1 and kind in ('ai', 'replay')
		  and status in ('generating', 'running'))
		+ (select count(*) from browser_sessions where workspace_id = $1 and closed_at is null
		  and created_at > now() - make_interval(secs => $3)) < $2`, wsID, workspaceCap, sessionMax.Seconds()).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("count open browsers: %w", err)
	}
	return ok, nil
}

func (m *model) createSession(ctx context.Context, id, wsID, issueID, userID, testerURL, token string) error {
	if _, err := m.db.Exec(ctx, `insert into browser_sessions (id, workspace_id, issue_id, started_by, tester_url, token)
		values ($1, $2, $3, $4, $5, $6)`, id, wsID, issueID, userID, testerURL, token); err != nil {
		return fmt.Errorf("save browser session: %w", err)
	}
	return nil
}

// session returns the tester URL and token of the user's open session in the workspace; errNotFound when there's
// none (another user's, closed, over its time limit, or on an issue they can no longer see).
func (m *model) session(ctx context.Context, wsID, id, userID string) (testerURL, token string, err error) {
	err = m.db.QueryRow(ctx, `select tester_url, token from browser_sessions b where id = $1 and workspace_id = $2
		and started_by = $3 and closed_at is null and created_at > now() - make_interval(secs => $4)
		and can_see_project((select project_id from issues where id = b.issue_id), $3)`,
		id, wsID, userID, sessionMax.Seconds()).Scan(&testerURL, &token)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", errNotFound
	}
	if err != nil {
		return "", "", fmt.Errorf("read browser session: %w", err)
	}
	return testerURL, token, nil
}

func (m *model) closeSession(ctx context.Context, id string) error {
	if _, err := m.db.Exec(ctx, `update browser_sessions set closed_at = now() where id = $1 and closed_at is null`, id); err != nil {
		return fmt.Errorf("close browser session: %w", err)
	}
	return nil
}
