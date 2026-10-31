package issue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/user/internal/doc"
	"github.com/devender07/tether/backend/user/internal/notify"
	"github.com/devender07/tether/backend/user/internal/realtime"
)

var (
	errNameTaken  = errors.New("name taken")
	errArchived   = errors.New("project archived")
	errBadOrder   = errors.New("order must list every status once")
	errInUse      = errors.New("status in use")
	errLastStatus = errors.New("last status")
	errLinked     = errors.New("issues already linked")
	errNoCycle    = errors.New("cycle not in the issue's project")
)

// parentError is the message for a parent that breaks the sub-issue rules.
type parentError string

func (e parentError) Error() string { return string(e) }

type Status struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Position int    `json:"position"`
	// Type is the status's own label for its category ("Reopen"); nil means the category's name. Only the statuses
	// endpoints fill it.
	Type *string `json:"type,omitempty"`
}

type Label struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type Person struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ProjectRef struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// CycleRef is the cycle an issue is in; State is "upcoming", "current" or "past" by today's date (UTC).
type CycleRef struct {
	ID       string `json:"id"`
	Number   int    `json:"number"`
	Name     string `json:"name"`
	StartsOn string `json:"starts_on"`
	EndsOn   string `json:"ends_on"`
	State    string `json:"state"`
}

type IssueRef struct {
	ID         string  `json:"id"`
	Identifier string  `json:"identifier"`
	Title      string  `json:"title"`
	Status     Status  `json:"status"`
	Assignee   *Person `json:"assignee"`
}

type Issue struct {
	ID          string          `json:"id"`
	Identifier  string          `json:"identifier"`
	Number      int             `json:"number"`
	Project     ProjectRef      `json:"project"`
	Parent      *IssueRef       `json:"parent"`
	Title       string          `json:"title"`
	Description json.RawMessage `json:"description"`
	Status      Status          `json:"status"`
	Assignee    *Person         `json:"assignee"`
	Priority    int             `json:"priority"`
	DueDate     *string         `json:"due_date"`
	Labels      []Label         `json:"labels"`
	Cycle       *CycleRef       `json:"cycle"`
	CreatedBy   *Person         `json:"created_by"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	// DescriptionGeneration counts REST writes of the description; the collab ready frame carries it too, so an
	// editor that was offline across one drops its stale document instead of merging it back.
	DescriptionGeneration int  `json:"description_generation"`
	CanEdit               bool `json:"can_edit"`
	CanDelete             bool `json:"can_delete"`
}

// IssueDetail is a single issue with its relations.
type IssueDetail struct {
	*Issue
	SubIssues []IssueRef `json:"sub_issues"`
	Blocks    []IssueRef `json:"blocks"`
	BlockedBy []IssueRef `json:"blocked_by"`
	Watching  bool       `json:"watching"`
	// PullRequests are the linked pull requests (gitlink's JSON), newest first.
	PullRequests json.RawMessage `json:"pull_requests"`
}

// fields are an issue's form values. A nil field is left unchanged on update (defaulted on create);
// an empty assigneeID, dueDate, parent or cycleID clears it. labelIDs replace the issue's labels only when setLabels is true,
// and description (nil clears it) replaces it only when setDescription is true.
type fields struct {
	title, statusID, assigneeID, dueDate, parent, cycleID *string
	priority                                              *int
	labelIDs                                              []string
	setLabels                                             bool
	description                                           json.RawMessage
	setDescription                                        bool
}

// refJSON is an issue ref as JSON, selected with refFrom.
const (
	refJSON = `json_build_object('id', r.id, 'identifier', rp.key || '-' || r.number, 'title', r.title,
		'status', json_build_object('id', rs.id, 'name', rs.name, 'category', rs.category, 'position', rs.position),
		'assignee', (select json_build_object('id', ru.id, 'name', ru.name) from users ru where ru.id = r.assignee_id))`
	refFrom = ` from issues r join projects rp on rp.id = r.project_id join statuses rs on rs.id = r.status_id`
	// cycleJSON is the issue's cycle ref; its state matches cycle's stateSQL.
	cycleJSON = `(select json_build_object('id', cy.id, 'number', cy.number, 'name', coalesce(cy.name, 'Cycle ' || cy.number),
		'starts_on', to_char(cy.starts_on, 'YYYY-MM-DD'), 'ends_on', to_char(cy.ends_on, 'YYYY-MM-DD'),
		'state', case when cy.starts_on > (now() at time zone 'utc')::date then 'upcoming'
		              when cy.ends_on < (now() at time zone 'utc')::date then 'past' else 'current' end)
		from cycles cy where cy.id = i.cycle_id)`
)

const (
	issueCols = issueHead + `i.description,` + issueTail
	// listCols leave the description out (null): lists don't show it and it's most of an issue's size.
	listCols  = issueHead + `null::jsonb,` + issueTail
	issueHead = `i.id::text, p.key || '-' || i.number, i.number, p.key, p.name,
	(select ` + refJSON + refFrom + ` where r.id = i.parent_id), i.title, `
	issueTail = `
	s.id::text, s.name, s.category, s.position, a.id::text, a.name, i.priority, to_char(i.due_date, 'YYYY-MM-DD'),
	coalesce((select json_agg(json_build_object('id', l.id, 'name', l.name, 'color', l.color) order by lower(l.name), l.id)
	  from issue_labels il join labels l on l.id = il.label_id where il.issue_id = i.id), '[]'), ` + cycleJSON + `,
	c.id::text, c.name, i.created_at, i.updated_at, i.description_generation
	from issues i join projects p on p.id = i.project_id join statuses s on s.id = i.status_id
	left join users a on a.id = i.assignee_id left join users c on c.id = i.created_by`
)

func scanIssue(row pgx.Row) (Issue, error) {
	var i Issue
	var aID, aName, cID, cName *string
	err := row.Scan(&i.ID, &i.Identifier, &i.Number, &i.Project.Key, &i.Project.Name, &i.Parent, &i.Title, &i.Description,
		&i.Status.ID, &i.Status.Name, &i.Status.Category, &i.Status.Position, &aID, &aName, &i.Priority, &i.DueDate,
		&i.Labels, &i.Cycle, &cID, &cName, &i.CreatedAt, &i.UpdatedAt, &i.DescriptionGeneration)
	if aID != nil {
		i.Assignee = &Person{*aID, *aName}
	}
	if cID != nil {
		i.CreatedBy = &Person{*cID, *cName}
	}
	i.CreatedAt, i.UpdatedAt = i.CreatedAt.UTC(), i.UpdatedAt.UTC()
	return i, err
}

// model owns every query for issues, statuses and labels.
type model struct{ db *pgxpool.Pool }

// names are what an issue's realtime events need, read in the change's transaction.
type names struct {
	wsID, slug, identifier, key string
	cycle                       *int     // its cycle's number
	related                     []string // identifiers of its parent, sub-issues and linked issues: their pages show it
}

func issueNames(ctx context.Context, tx pgx.Tx, id string) (names, error) {
	var n names
	err := tx.QueryRow(ctx,
		`select w.id::text, w.slug, p.key || '-' || i.number, p.key, (select number from cycles where id = i.cycle_id),
		        array(select rp.key || '-' || r.number from issues r join projects rp on rp.id = r.project_id
		              where r.id = i.parent_id or r.parent_id = i.id
		                 or r.id in (select blocked_id from issue_links where blocker_id = i.id
		                             union select blocker_id from issue_links where blocked_id = i.id))
		 from issues i join projects p on p.id = i.project_id join workspaces w on w.id = i.workspace_id
		 where i.id = $1`, id).Scan(&n.wsID, &n.slug, &n.identifier, &n.key, &n.cycle, &n.related)
	if err != nil {
		return n, fmt.Errorf("issue names: %w", err)
	}
	return n, nil
}

// event is kind for the issue's workspace, by the actor, with paths.
func (n names) event(kind, actorID string, paths ...string) realtime.Event {
	return realtime.Event{Kind: kind, Workspace: n.slug, Actor: actorID, Audience: realtime.ToWorkspace(n.wsID), Paths: paths}
}

// relatedPaths are the pages of the issue's parent, sub-issues and linked issues, and its cycle's (which count it).
func (n names) relatedPaths() []string {
	ps := make([]string, len(n.related))
	for i, r := range n.related {
		ps[i] = realtime.Issue(n.slug, r)
	}
	if n.cycle != nil {
		ps = append(ps, realtime.Cycles(n.slug, n.key), realtime.Cycle(n.slug, n.key, *n.cycle))
	}
	return ps
}

// publishIssue publishes kind about the issue as named before and after the change: its pages, lists and the
// pages of the issues related to it.
func publishIssue(ctx context.Context, tx pgx.Tx, kind, actorID string, ns ...names) error {
	var paths []string
	for _, n := range ns {
		paths = slices.Concat(paths, realtime.IssuePaths(n.slug, n.identifier, n.key), n.relatedPaths())
	}
	return realtime.Publish(ctx, tx, ns[0].event(kind, actorID, paths...))
}

// --- issues ---

// projectID returns the id of the project with the key; "" when there's none.
func (m *model) projectID(ctx context.Context, workspaceID, key string) (string, error) {
	var id string
	err := m.db.QueryRow(ctx, `select id::text from projects where workspace_id = $1 and key = canonical_key($1, $2)`, workspaceID, key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("find project: %w", err)
	}
	return id, nil
}

// maxList caps a list's issues.
// ponytail: newest maxList only, older ones are unreachable; add keyset pagination (number / created_at, id) when
// a project or assignee gets near it.
const maxList = 2000

// inCycle returns the cycle's issues, newest first.
func (m *model) inCycle(ctx context.Context, cycleID string) ([]Issue, error) {
	return m.issues(ctx, `i.cycle_id = $1 order by i.number desc`, cycleID)
}

// list returns the project's issues, newest first.
func (m *model) list(ctx context.Context, projectID string) ([]Issue, error) {
	return m.issues(ctx, `i.project_id = $1 order by i.number desc`, projectID)
}

// assigned returns the user's assigned issues in the workspace's active projects, newest first.
func (m *model) assigned(ctx context.Context, workspaceID, userID string) ([]Issue, error) {
	return m.issues(ctx, `i.workspace_id = $1 and i.assignee_id = $2 and p.archived_at is null
		order by i.created_at desc, i.id desc`, workspaceID, userID)
}

// issues selects up to maxList issues, without descriptions, matching where (a condition plus its order by).
func (m *model) issues(ctx context.Context, where string, args ...any) ([]Issue, error) {
	rows, err := m.db.Query(ctx, `select `+listCols+` where `+where+` limit `+strconv.Itoa(maxList), args...)
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	is, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Issue, error) { return scanIssue(row) })
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	return is, nil
}

// get returns the issue with the project key and number; nil when there's none.
// The key may be a project's old key (canonical_key resolves aliases), so issue URLs survive key changes.
func (m *model) get(ctx context.Context, workspaceID, key string, number int) (*Issue, error) {
	return oneIssue(m.db.QueryRow(ctx, `select `+issueCols+` where i.workspace_id = $1 and p.key = canonical_key($1, $2) and i.number = $3`,
		workspaceID, key, number))
}

func (m *model) byID(ctx context.Context, id string) (*Issue, error) {
	return oneIssue(m.db.QueryRow(ctx, `select `+issueCols+` where i.id = $1`, id))
}

// relations returns the issue's sub-issues (by number), the issues it blocks and is blocked by (oldest link first),
// whether the user watches it and its pull requests.
func (m *model) relations(ctx context.Context, i *Issue, userID string) (IssueDetail, error) {
	d := IssueDetail{Issue: i}
	err := m.db.QueryRow(ctx,
		`select coalesce((select json_agg(`+refJSON+` order by r.number)`+refFrom+` where r.parent_id = $1), '[]'),
		        coalesce((select json_agg(`+refJSON+` order by l.created_at, r.id)`+refFrom+`
		                  join issue_links l on l.blocked_id = r.id where l.blocker_id = $1), '[]'),
		        coalesce((select json_agg(`+refJSON+` order by l.created_at, r.id)`+refFrom+`
		                  join issue_links l on l.blocker_id = r.id where l.blocked_id = $1), '[]'),
		        exists(select 1 from issue_watchers where issue_id = $1 and user_id = $2),
		        coalesce((select json_agg(json_build_object('provider', pr.provider, 'repo', pr.repo, 'number', pr.number,
		                    'url', pr.url, 'title', pr.title, 'branch', pr.branch, 'author', pr.author, 'state', pr.state,
		                    'closes', ipr.closes, 'updated_at', pr.updated_at) order by pr.id desc)
		                  from issue_pull_requests ipr join pull_requests pr on pr.id = ipr.pr_id where ipr.issue_id = $1), '[]')`,
		i.ID, userID).Scan(&d.SubIssues, &d.Blocks, &d.BlockedBy, &d.Watching, &d.PullRequests)
	if err != nil {
		return d, fmt.Errorf("issue relations: %w", err)
	}
	return d, nil
}

// search returns up to 20 of the workspace's issues, limited to the project key unless it's "": with q "" the
// newest; otherwise the issue key-number first, then those whose title contains q, newest first.
func (m *model) search(ctx context.Context, workspaceID, project, q, key string, number int) ([]IssueRef, error) {
	rows, err := m.db.Query(ctx,
		`select `+refJSON+refFrom+`
		 where r.workspace_id = $1 and ($2 = '' or rp.key = canonical_key($1, $2))
		   and ($3 = '' or rp.key = canonical_key($1, $4) and r.number = $5 or r.title ilike '%' || $6 || '%')
		 order by rp.key = canonical_key($1, $4) and r.number = $5 desc, r.created_at desc, r.id desc limit 20`,
		workspaceID, project, q, key, number, escapeLike(q))
	if err != nil {
		return nil, fmt.Errorf("search issues: %w", err)
	}
	refs, err := pgx.CollectRows(rows, pgx.RowTo[IssueRef])
	if err != nil {
		return nil, fmt.Errorf("search issues: %w", err)
	}
	return refs, nil
}

// Hit is an issue found by the workspace search, with what matched ("identifier", "title" or "description"; nil
// for the recent issues an empty search returns).
type Hit struct {
	IssueRef
	Project ProjectRef `json:"project"`
	Match   *string    `json:"match"`
}

// ProjectHit is a project found by the workspace search.
type ProjectHit struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Archived bool   `json:"archived"`
}

const (
	hitJSON = `(` + refJSON + `)::jsonb || jsonb_build_object('project', jsonb_build_object('key', rp.key, 'name', rp.name),
		'match', (array['identifier', 'title', 'description'])[m.rank])`
	// identified is the id of the issue whose identifier is key $2, number $3 (old keys too); null when there's none.
	identified = `(select i.id from issues i join projects p on p.id = i.project_id
		where i.workspace_id = $1 and p.key = (select canonical_key($1, $2)) and i.number = $3)`
)

// searchAll finds the workspace's issues (up to 12) and projects (up to 8) for the palette. Issues match on their
// exact identifier first, then title (substring, or a trigram word match for typos), then description text; each
// kind newest first, with archived projects' issues after the rest. Projects match on key, then name. With q ""
// it returns the 8 most recently updated issues and the first 8 active projects.
func (m *model) searchAll(ctx context.Context, workspaceID, q, key string, number int) ([]Hit, []ProjectHit, error) {
	var hits []Hit
	var projects []ProjectHit
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var issueSQL, projectSQL string
		args, projectArgs := []any{workspaceID}, []any{workspaceID}
		if q == "" {
			issueSQL = `select ` + hitJSON + refFrom + ` cross join (select 0) m(rank)
				where r.workspace_id = $1 order by r.updated_at desc, r.id desc limit 8`
			projectSQL = `select key, name, false from projects where workspace_id = $1 and archived_at is null
				order by lower(name), key limit 8`
		} else {
			// <% uses the trigram indexes at this threshold; word_similarity's default (0.6) misses most typos.
			if _, err := tx.Exec(ctx, `select set_config('pg_trgm.word_similarity_threshold', '0.3', true)`); err != nil {
				return err
			}
			issueSQL = `select ` + hitJSON + refFrom + `
				cross join lateral (select case when r.id = ` + identified + ` then 1
				  when r.title ilike $4 or $5 <% r.title then 2 else 3 end) m(rank)
				where r.workspace_id = $1
				  and (r.id = ` + identified + ` or r.title ilike $4 or $5 <% r.title or r.description_text ilike $4)
				order by m.rank = 1 desc, rp.archived_at is not null, m.rank, r.updated_at desc, r.id desc limit 12`
			projectSQL = `select key, name, archived_at is not null from projects
				where workspace_id = $1 and (key = (select canonical_key($1, upper($3))) or name ilike $2 or $3 <% name)
				order by key = (select canonical_key($1, upper($3))) desc, archived_at is not null, name ilike $2 desc,
				  lower(name), key limit 8`
			pattern := "%" + escapeLike(q) + "%"
			args = append(args, key, number, pattern, q)
			projectArgs = append(projectArgs, pattern, q)
		}
		rows, err := tx.Query(ctx, issueSQL, args...)
		if err != nil {
			return err
		}
		if hits, err = pgx.CollectRows(rows, pgx.RowTo[Hit]); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, projectSQL, projectArgs...)
		if err != nil {
			return err
		}
		projects, err = pgx.CollectRows(rows, pgx.RowToStructByPos[ProjectHit])
		return err
	})
	if err != nil {
		return nil, nil, fmt.Errorf("search workspace: %w", err)
	}
	return hits, projects, nil
}

// escapeLike escapes LIKE's wildcards so s matches literally.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// checkRefs returns field errors for a status, assignee or labels that aren't the workspace's.
func (m *model) checkRefs(ctx context.Context, workspaceID string, f fields) (map[string]string, error) {
	var statusOK, assigneeOK bool
	var labels int
	assignee := f.assigneeID
	if assignee != nil && *assignee == "" {
		assignee = nil
	}
	err := m.db.QueryRow(ctx,
		`select $2::uuid is null or exists(select 1 from statuses where workspace_id = $1 and id = $2),
		        $3::uuid is null or exists(select 1 from workspace_members where workspace_id = $1 and user_id = $3),
		        (select count(*) from labels where workspace_id = $1 and id = any($4::uuid[]))`,
		workspaceID, f.statusID, assignee, f.labelIDs).Scan(&statusOK, &assigneeOK, &labels)
	if err != nil {
		return nil, fmt.Errorf("check issue refs: %w", err)
	}
	errs := map[string]string{}
	if !statusOK {
		errs["status_id"] = "Choose a status."
	}
	if !assigneeOK {
		errs["assignee_id"] = "Choose someone in this workspace."
	}
	if labels != len(f.labelIDs) {
		errs["label_ids"] = "Choose labels from this workspace."
	}
	return errs, nil
}

// create adds an issue numbered after the project's last one and returns its id; "" when there's no project with
// the key. Returns errArchived when the project is archived. Bumping issue_seq locks the project row, so concurrent
// creates get distinct numbers.
func (m *model) create(ctx context.Context, workspaceID, key, userID string, f fields) (string, error) {
	var id string
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var projectID string
		var number int
		var archived bool
		if err := tx.QueryRow(ctx,
			`update projects set issue_seq = issue_seq + 1 where workspace_id = $1 and key = canonical_key($1, $2)
			 returning id::text, issue_seq, archived_at is not null`, workspaceID, key).Scan(&projectID, &number, &archived); err != nil {
			return err
		}
		if archived {
			return errArchived
		}
		if err := checkCycle(ctx, tx, projectID, f.cycleID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx,
			`insert into issues (workspace_id, project_id, number, title, description, description_text, status_id, assignee_id, priority,
			   due_date, created_by, cycle_id)
			 values ($1, $2, $3, $4, $5, $11,
			         coalesce($6::uuid, (select id from statuses where workspace_id = $1 order by position, id limit 1)),
			         nullif($7, '')::uuid, coalesce($8::smallint, 0), nullif($9, '')::date, $10, nullif($12, '')::uuid)
			 returning id::text`,
			workspaceID, projectID, number, f.title, f.description, f.statusID, f.assigneeID, f.priority, f.dueDate, userID,
			doc.PlainText(f.description), f.cycleID).Scan(&id); err != nil {
			return err
		}
		if f.parent != nil {
			if err := setParent(ctx, tx, id, *f.parent); err != nil {
				return err
			}
		}
		if err := setLabels(ctx, tx, workspaceID, id, f.labelIDs); err != nil {
			return err
		}
		if err := addEvents(ctx, tx, id, userID, []event{{"created", struct{}{}}}); err != nil {
			return err
		}
		if err := notify.Record(ctx, tx, notify.Change{IssueID: id, ActorID: userID, Watch: []string{userID},
			Assignee: deref(f.assigneeID), Mentioned: doc.Mentions(f.description)}); err != nil {
			return err
		}
		n, err := issueNames(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := realtime.Publish(ctx, tx, n.event("issue.created", userID, slices.Concat(n.relatedPaths(),
			[]string{realtime.ProjectIssues(n.slug, n.key), realtime.Project(n.slug, n.key), realtime.MyIssues(n.slug),
				realtime.Activity(n.slug), realtime.ProjectStats(n.slug)})...)); err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, realtime.AdminStats(userID))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if errors.Is(err, errArchived) || errors.Is(err, errNoCycle) || errors.As(err, new(parentError)) {
		return "", err
	}
	if err != nil {
		return "", fmt.Errorf("create issue: %w", err)
	}
	return id, nil
}

// update changes the issue's non-nil fields, sets updated_at, and logs each real change as the actor's event, with
// its notifications. When it sets the description it also drops the issue's live-editing updates.
func (m *model) update(ctx context.Context, workspaceID, id, actorID string, f fields) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error { return apply(ctx, tx, workspaceID, id, actorID, f, nil) })
	if errors.Is(err, errNoCycle) || errors.As(err, new(parentError)) {
		return err
	}
	if err != nil {
		return fmt.Errorf("update issue: %w", err)
	}
	return nil
}

// SetStatus moves the issue to the status (one of its workspace's) in tx like a manual change, with no actor; the
// status event's data gains via (what moved it, e.g. a merged pull request).
func SetStatus(ctx context.Context, tx pgx.Tx, issueID, statusID string, via any) error {
	if err := apply(ctx, tx, "", issueID, "", fields{statusID: &statusID}, via); err != nil {
		return fmt.Errorf("set issue status: %w", err)
	}
	return nil
}

// apply is update in tx; actorID "" is no actor. A non-nil via is added to the status event's data.
func apply(ctx context.Context, tx pgx.Tx, workspaceID, id, actorID string, f fields, via any) error {
	before, err := snap(ctx, tx, id)
	if err != nil {
		return err
	}
	namedBefore, err := issueNames(ctx, tx, id)
	if err != nil {
		return err
	}
	if f.setDescription {
		// The live document is replaced: every instance resets the issue's editors so they reload it, and the next
		// one seeds it from the new description (a recent seed's claim would hold that off for 10s).
		if _, err := tx.Exec(ctx, `delete from issue_doc_updates where issue_id = $1`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `update issues set seed_claimed_at = null where id = $1`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `select pg_notify('collab', $1)`, notePrefix(id, noteReset)); err != nil {
			return err
		}
	}
	if f.parent != nil {
		if err := setParent(ctx, tx, id, *f.parent); err != nil {
			return err
		}
	}
	if err := checkCycle(ctx, tx, before.projectID, f.cycleID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`update issues set title = coalesce($2, title), description = case when $8 then $3::jsonb else description end,
		   description_text = case when $8 then $9 else description_text end,
		   description_generation = description_generation + case when $8 then 1 else 0 end,
		   status_id = coalesce($4::uuid, status_id),
		   assignee_id = case when $5::text is null then assignee_id else nullif($5, '')::uuid end,
		   priority = coalesce($6::smallint, priority),
		   due_date = case when $7::text is null then due_date else nullif($7, '')::date end,
		   cycle_id = case when $10::text is null then cycle_id else nullif($10, '')::uuid end,
		   updated_at = now()
		 where id = $1`,
		id, f.title, f.description, f.statusID, f.assigneeID, f.priority, f.dueDate, f.setDescription,
		doc.PlainText(f.description), f.cycleID); err != nil {
		return err
	}
	if f.setLabels {
		if _, err := tx.Exec(ctx, `delete from issue_labels where issue_id = $1`, id); err != nil {
			return err
		}
		if err := setLabels(ctx, tx, workspaceID, id, f.labelIDs); err != nil {
			return err
		}
	}
	after, err := snap(ctx, tx, id)
	if err != nil {
		return err
	}
	es := diff(before, after)
	for i, e := range es {
		if e.kind == "status" && via != nil {
			es[i].data = struct {
				change[statusSnap]
				Via any `json:"via"`
			}{e.data.(change[statusSnap]), via}
		}
	}
	if err := addEvents(ctx, tx, id, actorID, es); err != nil {
		return err
	}
	c := notify.Change{IssueID: id, ActorID: actorID, Mentioned: doc.NewMentions(before.description, after.description)}
	for _, e := range es {
		switch e.kind {
		case "status":
			c.Status = e.data
		case "assignee":
			c.Assignee = personID(after.assignee)
		}
	}
	if err := notify.Record(ctx, tx, c); err != nil {
		return err
	}
	namedAfter, err := issueNames(ctx, tx, id)
	if err != nil {
		return err
	}
	return publishIssue(ctx, tx, "issue.updated", actorID, namedBefore, namedAfter)
}

// snap reads the issue's logged fields, locking it.
func snap(ctx context.Context, tx pgx.Tx, id string) (snapshot, error) {
	var s snapshot
	err := tx.QueryRow(ctx,
		`select i.title, i.description, s.id::text, s.name, s.category,
		        (select json_build_object('id', u.id, 'name', u.name) from users u where u.id = i.assignee_id),
		        i.priority, to_char(i.due_date, 'YYYY-MM-DD'),
		        coalesce((select json_agg(json_build_object('id', l.id, 'name', l.name, 'color', l.color) order by lower(l.name), l.id)
		                  from issue_labels il join labels l on l.id = il.label_id where il.issue_id = i.id), '[]'),
		        (select json_build_object('id', r.id, 'identifier', rp.key || '-' || r.number, 'title', r.title)
		         from issues r join projects rp on rp.id = r.project_id where r.id = i.parent_id),
		        i.project_id::text, coalesce(i.cycle_id::text, ''),
		        (select json_build_object('number', cy.number, 'name', coalesce(cy.name, 'Cycle ' || cy.number))
		         from cycles cy where cy.id = i.cycle_id)
		 from issues i join statuses s on s.id = i.status_id where i.id = $1 for update of i`, id).
		Scan(&s.title, &s.description, &s.status.ID, &s.status.Name, &s.status.Category, &s.assignee, &s.priority, &s.dueDate,
			&s.labels, &s.parent, &s.projectID, &s.cycleID, &s.cycle)
	return s, err
}

// checkCycle returns errNoCycle unless the cycle id is "" (none), nil (unchanged) or one of the project's cycles,
// which it then holds against deletion until the transaction ends.
func checkCycle(ctx context.Context, tx pgx.Tx, projectID string, cycleID *string) error {
	if cycleID == nil || *cycleID == "" {
		return nil
	}
	var n int
	if err := tx.QueryRow(ctx, `select count(*) from (select 1 from cycles where id = $1 and project_id = $2 for key share) c`,
		*cycleID, projectID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return errNoCycle
	}
	return nil
}

// addEvents logs the events as the actor's. A description edit within 10 minutes of the actor's latest event on the
// issue, itself a description edit, bumps that event's time instead.
func addEvents(ctx context.Context, tx pgx.Tx, issueID, actorID string, es []event) error {
	actor := nullable(actorID)
	for _, e := range es {
		var err error
		if e.kind == "description" {
			_, err = tx.Exec(ctx,
				`with last as (select id from issue_events where issue_id = $1 and actor_id = $2 order by id desc limit 1),
				 bumped as (update issue_events e set created_at = now() from last
				            where e.id = last.id and e.kind = 'description' and e.created_at > now() - interval '10 minutes' returning e.id)
				 insert into issue_events (issue_id, actor_id, kind) select $1, $2, 'description' where not exists (select 1 from bumped)`,
				issueID, actor)
		} else {
			var data []byte
			if data, err = json.Marshal(e.data); err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `insert into issue_events (issue_id, actor_id, kind, data) values ($1, $2, $3, $4)`,
				issueID, actor, e.kind, data)
		}
		if err != nil {
			return fmt.Errorf("add %s event: %w", e.kind, err)
		}
	}
	return nil
}

// nullable is s, or nil (SQL null) when it's "".
func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// addLinkEvents logs a new or removed link on both issues, each from its own side.
func addLinkEvents(ctx context.Context, tx pgx.Tx, blockerID, blockedID, actorID string, removed bool) error {
	_, err := tx.Exec(ctx,
		`insert into issue_events (issue_id, actor_id, kind, data)
		 select v.id, $3, 'link', jsonb_build_object('type', v.type, 'removed', $4::bool,
		          'issue', jsonb_build_object('id', o.id, 'identifier', p.key || '-' || o.number, 'title', o.title))
		 from (values ($1::uuid, $2::uuid, 'blocks'), ($2::uuid, $1::uuid, 'blocked_by')) v(id, other, type)
		 join issues o on o.id = v.other join projects p on p.id = o.project_id`, blockerID, blockedID, actorID, removed)
	if err != nil {
		return fmt.Errorf("add link events: %w", err)
	}
	return nil
}

// setParent makes the issue with the id a sub-issue of the one with the identifier in the same workspace, or
// top-level when identifier is "". Returns a parentError when that breaks the rules. Locking both issues first
// keeps concurrent changes from nesting deeper than one level.
func setParent(ctx context.Context, tx pgx.Tx, id, identifier string) error {
	if identifier == "" {
		_, err := tx.Exec(ctx, `update issues set parent_id = null where id = $1`, id)
		return err
	}
	key, number, ok := parseIdentifier(identifier)
	var parentID string
	if ok {
		err := tx.QueryRow(ctx,
			`select i.id::text from issues i join projects p on p.id = i.project_id join issues c on c.workspace_id = i.workspace_id
			 where c.id = $1 and p.key = canonical_key(c.workspace_id, $2) and i.number = $3`, id, key, number).Scan(&parentID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	switch {
	case parentID == "":
		return parentError("Issue not found.")
	case parentID == id:
		return parentError("An issue can't be its own parent.")
	}
	if err := lockIssues(ctx, tx, id, parentID); err != nil {
		return err
	}
	var sameProject, nested, hasSubs bool
	if err := tx.QueryRow(ctx,
		`select c.project_id = p.project_id, p.parent_id is not null, exists(select 1 from issues where parent_id = c.id)
		 from issues c, issues p where c.id = $1 and p.id = $2`, id, parentID).Scan(&sameProject, &nested, &hasSubs); err != nil {
		return err
	}
	switch {
	case !sameProject:
		return parentError("Choose an issue in the same project.")
	case nested:
		return parentError("Sub-issues can't have sub-issues.")
	case hasSubs:
		return parentError("This issue has sub-issues, so it can't be one.")
	}
	_, err := tx.Exec(ctx, `update issues set parent_id = $2 where id = $1`, id, parentID)
	return err
}

// lockIssues locks the issues in id order, so two transactions locking the same pair can't deadlock.
func lockIssues(ctx context.Context, tx pgx.Tx, ids ...string) error {
	_, err := tx.Exec(ctx, `select 1 from issues where id = any($1::uuid[]) order by id for update`, ids)
	return err
}

// link records that blocker blocks blocked, as the actor. Returns errLinked when the two are already linked either way.
func (m *model) link(ctx context.Context, blockerID, blockedID, actorID string) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if err := lockIssues(ctx, tx, blockerID, blockedID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx,
			`insert into issue_links (blocker_id, blocked_id) select $1, $2
			 where not exists(select 1 from issue_links where blocker_id in ($1, $2) and blocked_id in ($1, $2))`,
			blockerID, blockedID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errLinked
		}
		if err := addLinkEvents(ctx, tx, blockerID, blockedID, actorID, false); err != nil {
			return err
		}
		return publishLink(ctx, tx, blockerID, blockedID, actorID)
	})
	if errors.Is(err, errLinked) {
		return err
	}
	if err != nil {
		return fmt.Errorf("link issues: %w", err)
	}
	return nil
}

// unlink removes the link between the two issues, whichever way it goes, as the actor; false when there's none.
func (m *model) unlink(ctx context.Context, a, b, actorID string) (bool, error) {
	found := false
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var blocker, blocked string
		err := tx.QueryRow(ctx,
			`delete from issue_links where blocker_id = $1 and blocked_id = $2 or blocker_id = $2 and blocked_id = $1
			 returning blocker_id::text, blocked_id::text`, a, b).Scan(&blocker, &blocked)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		if err := addLinkEvents(ctx, tx, blocker, blocked, actorID, true); err != nil {
			return err
		}
		return publishLink(ctx, tx, blocker, blocked, actorID)
	})
	if err != nil {
		return false, fmt.Errorf("unlink issues: %w", err)
	}
	return found, nil
}

// publishLink tells the workspace both linked issues' pages and timelines, and the activity feed, changed.
func publishLink(ctx context.Context, tx pgx.Tx, a, b, actorID string) error {
	var paths []string
	var n names
	for _, id := range []string{a, b} {
		var err error
		if n, err = issueNames(ctx, tx, id); err != nil {
			return err
		}
		paths = append(paths, realtime.Issue(n.slug, n.identifier), realtime.Timeline(n.slug, n.identifier))
	}
	return realtime.Publish(ctx, tx, n.event("issue.updated", actorID, append(paths, realtime.Activity(n.slug))...))
}

func setLabels(ctx context.Context, tx pgx.Tx, workspaceID, issueID string, labelIDs []string) error {
	if len(labelIDs) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx,
		`insert into issue_labels (issue_id, label_id)
		 select $2::uuid, id from labels where workspace_id = $1 and id = any($3::uuid[])`,
		workspaceID, issueID, labelIDs)
	return err
}

func (m *model) delete(ctx context.Context, id, actorID string) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		n, err := issueNames(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `delete from issues where id = $1`, id); err != nil {
			return err
		}
		if err := publishIssue(ctx, tx, "issue.deleted", actorID, n); err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, realtime.AdminStats(actorID))
	})
	if err != nil {
		return fmt.Errorf("delete issue: %w", err)
	}
	return nil
}

// --- live editing ---

type docUpdate struct {
	id   int64
	data []byte
}

// docUpdates returns the issue's stored Yjs updates, oldest first.
func (m *model) docUpdates(ctx context.Context, issueID string) ([]docUpdate, error) {
	rows, err := m.db.Query(ctx, `select id, data from issue_doc_updates where issue_id = $1 order by id`, issueID)
	if err != nil {
		return nil, fmt.Errorf("list doc updates: %w", err)
	}
	us, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (docUpdate, error) {
		var u docUpdate
		err := row.Scan(&u.id, &u.data)
		return u, err
	})
	if err != nil {
		return nil, fmt.Errorf("list doc updates: %w", err)
	}
	return us, nil
}

// addDocUpdate stores an update and returns its id; ok is false when the issue's stored updates would pass maxStored.
func (m *model) addDocUpdate(ctx context.Context, issueID string, data []byte) (id int64, ok bool, err error) {
	err = m.db.QueryRow(ctx,
		`insert into issue_doc_updates (issue_id, data)
		 select $1, $2 where (select coalesce(sum(octet_length(data)), 0) from issue_doc_updates where issue_id = $1)
		                     + octet_length($2::bytea) <= $3
		 returning id`, issueID, data, maxStored).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("add doc update: %w", err)
	}
	return id, true, nil
}

// docUpdate returns one stored update's data; nil when it's gone (compacted).
func (m *model) docUpdate(ctx context.Context, id int64) ([]byte, error) {
	var data []byte
	err := m.db.QueryRow(ctx, `select data from issue_doc_updates where id = $1`, id).Scan(&data)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("load doc update: %w", err)
	}
	return data, nil
}

// notify sends payload to every API instance listening on collab.
func (m *model) notify(ctx context.Context, payload string) error {
	if _, err := m.db.Exec(ctx, `select pg_notify('collab', $1)`, payload); err != nil {
		return fmt.Errorf("notify collab: %w", err)
	}
	return nil
}

// claimSeed claims seeding the issue's live document when nothing is stored yet and no fresh claim exists.
// A concurrent claim waits on the row lock, then sees the new seed_claimed_at and fails.
func (m *model) claimSeed(ctx context.Context, issueID string) (bool, error) {
	tag, err := m.db.Exec(ctx,
		`update issues set seed_claimed_at = now()
		 where id = $1 and (seed_claimed_at is null or seed_claimed_at < now() - interval '10 seconds')
		   and not exists (select 1 from issue_doc_updates where issue_id = $1)`, issueID)
	if err != nil {
		return false, fmt.Errorf("claim seed: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// renewSeed extends a seed claim that's still held.
func (m *model) renewSeed(ctx context.Context, issueID string) error {
	_, err := m.db.Exec(ctx, `update issues set seed_claimed_at = now() where id = $1 and seed_claimed_at is not null`, issueID)
	if err != nil {
		return fmt.Errorf("renew seed claim: %w", err)
	}
	return nil
}

// setSeedClaim refreshes the seed claim (handed to another editor here) or clears it (free for any instance).
func (m *model) setSeedClaim(ctx context.Context, issueID string, claimed bool) error {
	_, err := m.db.Exec(ctx, `update issues set seed_claimed_at = case when $2 then now() end where id = $1`, issueID, claimed)
	if err != nil {
		return fmt.Errorf("set seed claim: %w", err)
	}
	return nil
}

// compactDoc replaces the issue's updates with the ids, the ones the client loaded, with data, their merged state.
// Deleting by id rather than "up to" keeps an update that committed after the client's load.
func (m *model) compactDoc(ctx context.Context, issueID string, ids []int64, data []byte) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `delete from issue_doc_updates where issue_id = $1 and id = any($2)`, issueID, ids); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `insert into issue_doc_updates (issue_id, data) values ($1, $2)`, issueID, data)
		return err
	})
	if err != nil {
		return fmt.Errorf("compact doc: %w", err)
	}
	return nil
}

// saveDescription sets the issue's description (nil clears it), logging the actor's edit and notifying the newly
// mentioned when it changed.
func (m *model) saveDescription(ctx context.Context, issueID, actorID string, d json.RawMessage) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var old json.RawMessage
		var changed bool
		if err := tx.QueryRow(ctx,
			`update issues i set description = $2, description_text = $3, updated_at = now()
			 from (select id, description from issues where id = $1 for update) o where i.id = o.id
			 returning o.description, o.description is distinct from $2::jsonb`, issueID, d, doc.PlainText(d)).Scan(&old, &changed); err != nil {
			return err
		}
		if !changed {
			return nil
		}
		if err := addEvents(ctx, tx, issueID, actorID, []event{{"description", struct{}{}}}); err != nil {
			return err
		}
		if err := notify.Record(ctx, tx, notify.Change{IssueID: issueID, ActorID: actorID, Mentioned: doc.NewMentions(old, d)}); err != nil {
			return err
		}
		// The description itself is live over collab already, and lists don't show it.
		n, err := issueNames(ctx, tx, issueID)
		if err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, n.event("issue.updated", actorID, realtime.Timeline(n.slug, n.identifier)))
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("save description: %w", err)
	}
	return nil
}

// --- comments and activity ---

type Comment struct {
	ID        string          `json:"id"`
	Author    *Person         `json:"author"`
	Body      json.RawMessage `json:"body"`
	CreatedAt time.Time       `json:"created_at"`
	EditedAt  *time.Time      `json:"edited_at"`
	CanEdit   bool            `json:"can_edit"`
	CanDelete bool            `json:"can_delete"`
	issueID   string
	archived  bool // its issue's project is archived
}

type Event struct {
	ID        string          `json:"id"`
	Actor     *Person         `json:"actor"`
	Kind      string          `json:"kind"`
	Data      json.RawMessage `json:"data"`
	CreatedAt time.Time       `json:"created_at"`
}

const commentCols = `c.id::text, (select json_build_object('id', u.id, 'name', u.name) from users u where u.id = c.author_id),
	c.body, c.created_at, c.edited_at, c.issue_id::text, p.archived_at is not null
	from comments c join issues i on i.id = c.issue_id join projects p on p.id = i.project_id`

func scanComment(row pgx.Row) (Comment, error) {
	var c Comment
	err := row.Scan(&c.ID, &c.Author, &c.Body, &c.CreatedAt, &c.EditedAt, &c.issueID, &c.archived)
	c.CreatedAt = c.CreatedAt.UTC()
	if c.EditedAt != nil {
		*c.EditedAt = c.EditedAt.UTC()
	}
	return c, err
}

// timelinePage caps a timeline page's comments and events together.
const timelinePage = 100

// timeline returns a page of the issue's comments and events: the newest timelinePage created before before (all
// when nil), each oldest first, and whether there are older ones. A page never splits a created_at that's shared
// (one change logs several events at the same time), so the next page, before its oldest item, misses none.
func (m *model) timeline(ctx context.Context, issueID string, before *time.Time) ([]Comment, []Event, bool, error) {
	rows, err := m.db.Query(ctx, `select `+commentCols+` where c.issue_id = $1 and ($2::timestamptz is null or c.created_at < $2)
		order by c.created_at desc, c.id desc limit $3`, issueID, before, timelinePage+1)
	if err != nil {
		return nil, nil, false, fmt.Errorf("list comments: %w", err)
	}
	cs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Comment, error) { return scanComment(row) })
	if err != nil {
		return nil, nil, false, fmt.Errorf("list comments: %w", err)
	}
	rows, err = m.db.Query(ctx,
		`select e.id::text, (select json_build_object('id', u.id, 'name', u.name) from users u where u.id = e.actor_id),
		        e.kind, e.data, e.created_at
		 from issue_events e where e.issue_id = $1 and ($2::timestamptz is null or e.created_at < $2)
		 order by e.created_at desc, e.id desc limit $3`, issueID, before, timelinePage+1)
	if err != nil {
		return nil, nil, false, fmt.Errorf("list events: %w", err)
	}
	es, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Event, error) {
		var e Event
		err := row.Scan(&e.ID, &e.Actor, &e.Kind, &e.Data, &e.CreatedAt)
		e.CreatedAt = e.CreatedAt.UTC()
		return e, err
	})
	if err != nil {
		return nil, nil, false, fmt.Errorf("list events: %w", err)
	}
	cs, es, more := timelineCut(cs, es)
	slices.Reverse(cs)
	slices.Reverse(es)
	return cs, es, more, nil
}

// timelineCut keeps the newest timelinePage of cs and es (each newest first), dropping the whole created_at group the
// next one is in, and reports whether any were dropped.
// ponytail: when more than a page shares the newest created_at, that group is returned whole (up to a page of each
// kind) and older ones in it are skipped; only a bulk import could do that.
func timelineCut(cs []Comment, es []Event) ([]Comment, []Event, bool) {
	times := make([]time.Time, 0, len(cs)+len(es))
	for _, c := range cs {
		times = append(times, c.CreatedAt)
	}
	for _, e := range es {
		times = append(times, e.CreatedAt)
	}
	if len(times) <= timelinePage {
		return cs, es, false
	}
	slices.SortFunc(times, func(a, b time.Time) int { return b.Compare(a) })
	cut := times[timelinePage]
	keep := func(t time.Time) bool { return t.After(cut) }
	if times[0].Equal(cut) {
		keep = func(t time.Time) bool { return t.Equal(cut) }
	}
	cs = slices.DeleteFunc(cs, func(c Comment) bool { return !keep(c.CreatedAt) })
	es = slices.DeleteFunc(es, func(e Event) bool { return !keep(e.CreatedAt) })
	return cs, es, true
}

// Activity is one item of the workspace's activity feed: an event (Kind and Data, as on the timeline) or a comment
// (Excerpt).
type Activity struct {
	Type      string          `json:"type"` // "event" or "comment"
	ID        string          `json:"id"`
	CreatedAt time.Time       `json:"created_at"`
	Actor     *Person         `json:"actor"`
	Issue     json.RawMessage `json:"issue"` // {identifier, title, status}
	Kind      string          `json:"kind,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	Excerpt   *string         `json:"excerpt,omitempty"`
}

// activityPage caps an activity page.
const activityPage = 30

// activity returns a page of the workspace's events (but description edits) and comments on issues in active
// projects, newest first: the newest activityPage created before before (all when nil), and whether there are older
// ones. Like the timeline, a page never splits a shared created_at.
// ponytail: the created_at indexes are workspace-wide, so a quiet workspace's page scans other workspaces' newer rows;
// denormalise workspace_id onto issue_events and comments with (workspace_id, created_at) indexes when that shows up.
func (m *model) activity(ctx context.Context, workspaceID string, before *time.Time) ([]Activity, bool, error) {
	const issue = `json_build_object('identifier', p.key || '-' || i.number, 'title', i.title, 'status',
		json_build_object('id', s.id, 'name', s.name, 'category', s.category, 'position', s.position))`
	const actor = `(select json_build_object('id', u.id, 'name', u.name) from users u where u.id = %s)`
	const from = ` join issues i on i.id = %s.issue_id join projects p on p.id = i.project_id join statuses s on s.id = i.status_id
		 where i.workspace_id = $1 and p.archived_at is null and ($2::timestamptz is null or %[1]s.created_at < $2)`
	rows, err := m.db.Query(ctx,
		`(select 'event', e.id::text, e.created_at, `+fmt.Sprintf(actor, "e.actor_id")+`, `+issue+`, e.kind, e.data, null::jsonb
		  from issue_events e`+fmt.Sprintf(from, "e")+` and e.kind <> 'description'
		  order by e.created_at desc, e.id desc limit $3)
		 union all
		 (select 'comment', c.id::text, c.created_at, `+fmt.Sprintf(actor, "c.author_id")+`, `+issue+`, '', null, c.body
		  from comments c`+fmt.Sprintf(from, "c")+`
		  order by c.created_at desc, c.id desc limit $3)
		 order by 3 desc, 2 desc limit $3`, workspaceID, before, activityPage+1)
	if err != nil {
		return nil, false, fmt.Errorf("list activity: %w", err)
	}
	as, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Activity, error) {
		var a Activity
		var body []byte
		err := row.Scan(&a.Type, &a.ID, &a.CreatedAt, &a.Actor, &a.Issue, &a.Kind, &a.Data, &body)
		a.CreatedAt = a.CreatedAt.UTC()
		if a.Type == "comment" {
			r := []rune(strings.TrimSpace(doc.PlainText(body)))
			excerpt := string(r[:min(len(r), 200)])
			a.Excerpt = &excerpt
		}
		return a, err
	})
	if err != nil {
		return nil, false, fmt.Errorf("list activity: %w", err)
	}
	as, more := activityCut(as)
	return as, more, nil
}

// activityCut keeps the newest activityPage of as (newest first), dropping the whole created_at group the next one is
// in, and reports whether any were dropped. A page that's all one created_at is kept whole (see timelineCut).
func activityCut(as []Activity) ([]Activity, bool) {
	if len(as) <= activityPage {
		return as, false
	}
	cut, n := as[activityPage].CreatedAt, activityPage
	for n > 0 && as[n-1].CreatedAt.Equal(cut) {
		n--
	}
	if n == 0 {
		n = activityPage
	}
	return as[:n], true
}

// comment returns the workspace's comment with the id; nil when there's none.
func (m *model) comment(ctx context.Context, workspaceID, id string) (*Comment, error) {
	if !isUUID(id) {
		return nil, nil
	}
	c, err := scanComment(m.db.QueryRow(ctx, `select `+commentCols+` where i.workspace_id = $1 and c.id = $2`, workspaceID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("comment: %w", err)
	}
	return &c, nil
}

// archived reports whether the issue's project is archived.
func (m *model) archived(ctx context.Context, issueID string) (bool, error) {
	var archived bool
	err := m.db.QueryRow(ctx,
		`select p.archived_at is not null from issues i join projects p on p.id = i.project_id where i.id = $1`, issueID).Scan(&archived)
	if err != nil {
		return false, fmt.Errorf("project archived: %w", err)
	}
	return archived, nil
}

// createComment adds the author's comment and notifies the issue's watchers and the mentioned; the author starts
// watching.
func (m *model) createComment(ctx context.Context, issueID, authorID string, body json.RawMessage) (string, error) {
	var id string
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `insert into comments (issue_id, author_id, body) values ($1, $2, $3) returning id::text`,
			issueID, authorID, body).Scan(&id); err != nil {
			return err
		}
		if err := notify.Record(ctx, tx, notify.Change{IssueID: issueID, ActorID: authorID, Watch: []string{authorID},
			CommentID: id, Excerpt: notify.Excerpt(doc.PlainText(body)), NewComment: true, Mentioned: doc.Mentions(body)}); err != nil {
			return err
		}
		if err := publishComment(ctx, tx, issueID, authorID); err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, realtime.AdminStats(authorID))
	})
	if err != nil {
		return "", fmt.Errorf("create comment: %w", err)
	}
	return id, nil
}

// updateComment replaces the body of the issue's comment, marks it edited and notifies the newly mentioned.
func (m *model) updateComment(ctx context.Context, id, issueID, actorID string, body json.RawMessage) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var old json.RawMessage
		if err := tx.QueryRow(ctx,
			`update comments c set body = $2, edited_at = now()
			 from (select id, body from comments where id = $1 for update) o where c.id = o.id
			 returning o.body`, id, body).Scan(&old); err != nil {
			return err
		}
		if err := notify.Record(ctx, tx, notify.Change{IssueID: issueID, ActorID: actorID, CommentID: id,
			Excerpt: notify.Excerpt(doc.PlainText(body)), Mentioned: doc.NewMentions(old, body)}); err != nil {
			return err
		}
		return publishComment(ctx, tx, issueID, actorID)
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("update comment: %w", err)
	}
	return nil
}

// publishComment tells the workspace the issue's timeline and the activity feed changed.
func publishComment(ctx context.Context, tx pgx.Tx, issueID, actorID string) error {
	n, err := issueNames(ctx, tx, issueID)
	if err != nil {
		return err
	}
	return realtime.Publish(ctx, tx, n.event("comment.changed", actorID, realtime.Timeline(n.slug, n.identifier), realtime.Activity(n.slug)))
}

// setWatching starts or stops the user watching the issue, telling their other tabs.
func (m *model) setWatching(ctx context.Context, issueID, userID string, on bool) error {
	sql := `delete from issue_watchers where issue_id = $1 and user_id = $2`
	if on {
		sql = `insert into issue_watchers (issue_id, user_id) values ($1, $2) on conflict do nothing`
	}
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, sql, issueID, userID); err != nil {
			return err
		}
		n, err := issueNames(ctx, tx, issueID)
		if err != nil {
			return err
		}
		e := n.event("issue.watch", userID, realtime.Issue(n.slug, n.identifier))
		e.Audience = realtime.ToUser(userID)
		return realtime.Publish(ctx, tx, e)
	})
	if err != nil {
		return fmt.Errorf("set watching: %w", err)
	}
	return nil
}

func (m *model) deleteComment(ctx context.Context, id, issueID, actorID string) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `delete from comments where id = $1`, id); err != nil {
			return err
		}
		if err := publishComment(ctx, tx, issueID, actorID); err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, realtime.AdminStats(actorID))
	})
	if err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}
	return nil
}

func oneIssue(row pgx.Row) (*Issue, error) {
	i, err := scanIssue(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("issue: %w", err)
	}
	return &i, nil
}

// --- statuses ---

const statusCols = `id::text, name, category, position, type`

func scanStatus(row pgx.Row) (Status, error) {
	var s Status
	err := row.Scan(&s.ID, &s.Name, &s.Category, &s.Position, &s.Type)
	return s, err
}

func (m *model) listStatuses(ctx context.Context, workspaceID string) ([]Status, error) {
	rows, err := m.db.Query(ctx, `select `+statusCols+` from statuses where workspace_id = $1 order by position, id`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list statuses: %w", err)
	}
	ss, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Status, error) { return scanStatus(row) })
	if err != nil {
		return nil, fmt.Errorf("list statuses: %w", err)
	}
	return ss, nil
}

// publishing runs fn in a transaction that publishes e when fn succeeds.
func (m *model) publishing(ctx context.Context, e realtime.Event, fn func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if err := fn(tx); err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, e)
	})
}

// createStatus appends a status after the last one, publishing e. Returns errNameTaken when the name is in use.
func (m *model) createStatus(ctx context.Context, workspaceID, name, category string, typ *string, e realtime.Event) (Status, error) {
	var s Status
	err := m.publishing(ctx, e, func(tx pgx.Tx) error {
		var err error
		s, err = scanStatus(tx.QueryRow(ctx,
			`insert into statuses (workspace_id, name, category, type, position)
			 values ($1, $2, $3, $4, (select coalesce(max(position) + 1, 0) from statuses where workspace_id = $1))
			 returning `+statusCols, workspaceID, name, category, typ))
		return err
	})
	if isUniqueViolation(err) {
		return s, errNameTaken
	}
	if err != nil {
		return s, fmt.Errorf("create status: %w", err)
	}
	return s, nil
}

// updateStatus renames and recategorizes a status, publishing e; nil when there's none. Returns errNameTaken when the
// name is in use.
func (m *model) updateStatus(ctx context.Context, workspaceID, id, name, category string, typ *string, e realtime.Event) (*Status, error) {
	if !isUUID(id) {
		return nil, nil
	}
	var s Status
	err := m.publishing(ctx, e, func(tx pgx.Tx) error {
		var err error
		s, err = scanStatus(tx.QueryRow(ctx,
			`update statuses set name = $3, category = $4, type = $5 where workspace_id = $1 and id = $2 returning `+statusCols,
			workspaceID, id, name, category, typ))
		return err
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil
	case isUniqueViolation(err):
		return nil, errNameTaken
	case err != nil:
		return nil, fmt.Errorf("update status: %w", err)
	}
	return &s, nil
}

// lockStatuses returns the workspace's status ids, locking them against concurrent reorders and deletes.
func lockStatuses(ctx context.Context, tx pgx.Tx, workspaceID string) ([]string, error) {
	rows, err := tx.Query(ctx, `select id::text from statuses where workspace_id = $1 for update`, workspaceID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// reorderStatuses sets positions to the order of ids, publishing e. Returns errBadOrder unless ids is every status once.
func (m *model) reorderStatuses(ctx context.Context, workspaceID string, ids []string, e realtime.Event) error {
	err := m.publishing(ctx, e, func(tx pgx.Tx) error {
		have, err := lockStatuses(ctx, tx, workspaceID)
		if err != nil {
			return err
		}
		want := slices.Clone(ids)
		slices.Sort(have)
		slices.Sort(want)
		if !slices.Equal(have, want) {
			return errBadOrder
		}
		_, err = tx.Exec(ctx,
			`update statuses s set position = o.ord - 1 from unnest($2::uuid[]) with ordinality o(id, ord)
			 where s.workspace_id = $1 and s.id = o.id`, workspaceID, ids)
		return err
	})
	if errors.Is(err, errBadOrder) {
		return err
	}
	if err != nil {
		return fmt.Errorf("reorder statuses: %w", err)
	}
	return nil
}

// deleteStatus deletes a status, moving its issues to moveTo, and publishes e (every workspace path too when issues
// moved); false when there's no such status. Returns errLastStatus for the workspace's only status, and errInUse when
// issues use it and moveTo isn't another of the workspace's statuses.
func (m *model) deleteStatus(ctx context.Context, workspaceID, id, moveTo string, e realtime.Event) (bool, error) {
	found := false
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		ids, err := lockStatuses(ctx, tx, workspaceID)
		if err != nil || !slices.Contains(ids, id) {
			return err
		}
		found = true
		if len(ids) == 1 {
			return errLastStatus
		}
		var moved pgconn.CommandTag
		if isUUID(moveTo) {
			moved, err = tx.Exec(ctx,
				`update issues i set status_id = s.id from statuses s
				 where i.status_id = $2 and s.workspace_id = $1 and s.id = $3 and s.id <> $2`,
				workspaceID, id, moveTo)
			if err != nil {
				return err
			}
		}
		var inUse bool
		if err := tx.QueryRow(ctx, `select exists(select 1 from issues where status_id = $1)`, id).Scan(&inUse); err != nil {
			return err
		}
		if inUse {
			return errInUse
		}
		if _, err = tx.Exec(ctx, `delete from statuses where id = $1`, id); err != nil {
			return err
		}
		if moved.RowsAffected() > 0 {
			e.Paths = append(e.Paths, realtime.All(e.Workspace))
		}
		return realtime.Publish(ctx, tx, e)
	})
	if errors.Is(err, errLastStatus) || errors.Is(err, errInUse) {
		return true, err
	}
	if err != nil {
		return false, fmt.Errorf("delete status: %w", err)
	}
	return found, nil
}

// --- labels ---

const labelCols = `id::text, name, color`

func scanLabel(row pgx.Row) (Label, error) {
	var l Label
	err := row.Scan(&l.ID, &l.Name, &l.Color)
	return l, err
}

func (m *model) listLabels(ctx context.Context, workspaceID string) ([]Label, error) {
	rows, err := m.db.Query(ctx, `select `+labelCols+` from labels where workspace_id = $1 order by lower(name), id`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list labels: %w", err)
	}
	ls, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Label, error) { return scanLabel(row) })
	if err != nil {
		return nil, fmt.Errorf("list labels: %w", err)
	}
	return ls, nil
}

// createLabel publishes e. Returns errNameTaken when the name is in use.
func (m *model) createLabel(ctx context.Context, workspaceID, name, color string, e realtime.Event) (Label, error) {
	var l Label
	err := m.publishing(ctx, e, func(tx pgx.Tx) error {
		var err error
		l, err = scanLabel(tx.QueryRow(ctx,
			`insert into labels (workspace_id, name, color) values ($1, $2, $3) returning `+labelCols, workspaceID, name, color))
		return err
	})
	if isUniqueViolation(err) {
		return l, errNameTaken
	}
	if err != nil {
		return l, fmt.Errorf("create label: %w", err)
	}
	return l, nil
}

// updateLabel publishes e. It returns nil when there's no such label, and errNameTaken when the name is in use.
func (m *model) updateLabel(ctx context.Context, workspaceID, id, name, color string, e realtime.Event) (*Label, error) {
	if !isUUID(id) {
		return nil, nil
	}
	var l Label
	err := m.publishing(ctx, e, func(tx pgx.Tx) error {
		var err error
		l, err = scanLabel(tx.QueryRow(ctx,
			`update labels set name = $3, color = $4 where workspace_id = $1 and id = $2 returning `+labelCols,
			workspaceID, id, name, color))
		return err
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil
	case isUniqueViolation(err):
		return nil, errNameTaken
	case err != nil:
		return nil, fmt.Errorf("update label: %w", err)
	}
	return &l, nil
}

// deleteLabel deletes a label, which removes it from its issues, and publishes e; false when there's none.
func (m *model) deleteLabel(ctx context.Context, workspaceID, id string, e realtime.Event) (bool, error) {
	if !isUUID(id) {
		return false, nil
	}
	found := false
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `delete from labels where workspace_id = $1 and id = $2`, workspaceID, id)
		if found = tag.RowsAffected() == 1; err != nil || !found {
			return err
		}
		return realtime.Publish(ctx, tx, e)
	})
	if err != nil {
		return false, fmt.Errorf("delete label: %w", err)
	}
	return found, nil
}

func isUniqueViolation(err error) bool {
	pgErr := (*pgconn.PgError)(nil)
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
