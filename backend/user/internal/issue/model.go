package issue

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	errNameTaken  = errors.New("name taken")
	errArchived   = errors.New("project archived")
	errBadOrder   = errors.New("order must list every status once")
	errInUse      = errors.New("status in use")
	errLastStatus = errors.New("last status")
	errLinked     = errors.New("issues already linked")
)

// parentError is the message for a parent that breaks the sub-issue rules.
type parentError string

func (e parentError) Error() string { return string(e) }

type Status struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Position int    `json:"position"`
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

type IssueRef struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	Status     Status `json:"status"`
}

type Issue struct {
	ID          string     `json:"id"`
	Identifier  string     `json:"identifier"`
	Number      int        `json:"number"`
	Project     ProjectRef `json:"project"`
	Parent      *IssueRef  `json:"parent"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      Status     `json:"status"`
	Assignee    *Person    `json:"assignee"`
	Priority    int        `json:"priority"`
	DueDate     *string    `json:"due_date"`
	Labels      []Label    `json:"labels"`
	CreatedBy   *Person    `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CanEdit     bool       `json:"can_edit"`
	CanDelete   bool       `json:"can_delete"`
}

// IssueDetail is a single issue with its relations.
type IssueDetail struct {
	*Issue
	SubIssues []IssueRef `json:"sub_issues"`
	Blocks    []IssueRef `json:"blocks"`
	BlockedBy []IssueRef `json:"blocked_by"`
}

// fields are an issue's form values. A nil field is left unchanged on update (defaulted on create);
// an empty assigneeID, dueDate or parent clears it. labelIDs replace the issue's labels only when setLabels is true.
type fields struct {
	title, description, statusID, assigneeID, dueDate, parent *string
	priority                                                  *int
	labelIDs                                                  []string
	setLabels                                                 bool
}

// refJSON is an issue ref as JSON, selected with refFrom.
const (
	refJSON = `json_build_object('id', r.id, 'identifier', rp.key || '-' || r.number, 'title', r.title,
		'status', json_build_object('id', rs.id, 'name', rs.name, 'category', rs.category, 'position', rs.position))`
	refFrom = ` from issues r join projects rp on rp.id = r.project_id join statuses rs on rs.id = r.status_id`
)

const issueCols = `i.id::text, p.key || '-' || i.number, i.number, p.key, p.name,
	(select ` + refJSON + refFrom + ` where r.id = i.parent_id), i.title, i.description,
	s.id::text, s.name, s.category, s.position, a.id::text, a.name, i.priority, to_char(i.due_date, 'YYYY-MM-DD'),
	coalesce((select json_agg(json_build_object('id', l.id, 'name', l.name, 'color', l.color) order by lower(l.name), l.id)
	  from issue_labels il join labels l on l.id = il.label_id where il.issue_id = i.id), '[]'),
	c.id::text, c.name, i.created_at, i.updated_at
	from issues i join projects p on p.id = i.project_id join statuses s on s.id = i.status_id
	left join users a on a.id = i.assignee_id left join users c on c.id = i.created_by`

func scanIssue(row pgx.Row) (Issue, error) {
	var i Issue
	var aID, aName, cID, cName *string
	err := row.Scan(&i.ID, &i.Identifier, &i.Number, &i.Project.Key, &i.Project.Name, &i.Parent, &i.Title, &i.Description,
		&i.Status.ID, &i.Status.Name, &i.Status.Category, &i.Status.Position, &aID, &aName, &i.Priority, &i.DueDate,
		&i.Labels, &cID, &cName, &i.CreatedAt, &i.UpdatedAt)
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

// --- issues ---

// projectID returns the id of the project with the key; "" when there's none.
func (m *model) projectID(ctx context.Context, workspaceID, key string) (string, error) {
	var id string
	err := m.db.QueryRow(ctx, `select id::text from projects where workspace_id = $1 and key = $2`, workspaceID, key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("find project: %w", err)
	}
	return id, nil
}

// list returns the project's issues, newest first.
func (m *model) list(ctx context.Context, projectID string) ([]Issue, error) {
	rows, err := m.db.Query(ctx, `select `+issueCols+` where i.project_id = $1 order by i.number desc`, projectID)
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
// ponytail: matches the project's current key only, so renaming a key breaks old issue URLs; keep old keys as aliases if that bites.
func (m *model) get(ctx context.Context, workspaceID, key string, number int) (*Issue, error) {
	return oneIssue(m.db.QueryRow(ctx, `select `+issueCols+` where i.workspace_id = $1 and p.key = $2 and i.number = $3`,
		workspaceID, key, number))
}

func (m *model) byID(ctx context.Context, id string) (*Issue, error) {
	return oneIssue(m.db.QueryRow(ctx, `select `+issueCols+` where i.id::text = $1`, id))
}

// relations returns the issue's sub-issues (by number) and the issues it blocks and is blocked by (oldest link first).
func (m *model) relations(ctx context.Context, i *Issue) (IssueDetail, error) {
	d := IssueDetail{Issue: i}
	err := m.db.QueryRow(ctx,
		`select coalesce((select json_agg(`+refJSON+` order by r.number)`+refFrom+` where r.parent_id = $1), '[]'),
		        coalesce((select json_agg(`+refJSON+` order by l.created_at, r.id)`+refFrom+`
		                  join issue_links l on l.blocked_id = r.id where l.blocker_id = $1), '[]'),
		        coalesce((select json_agg(`+refJSON+` order by l.created_at, r.id)`+refFrom+`
		                  join issue_links l on l.blocker_id = r.id where l.blocked_id = $1), '[]')`,
		i.ID).Scan(&d.SubIssues, &d.Blocks, &d.BlockedBy)
	if err != nil {
		return d, fmt.Errorf("issue relations: %w", err)
	}
	return d, nil
}

// search returns up to 20 of the workspace's issues, limited to the project key unless it's "": with q "" the
// newest; otherwise the issue key-number first, then those whose title contains q, newest first.
// ponytail: ILIKE scan; add a trigram or full-text index when search gets slow (Phase 5).
func (m *model) search(ctx context.Context, workspaceID, project, q, key string, number int) ([]IssueRef, error) {
	rows, err := m.db.Query(ctx,
		`select `+refJSON+refFrom+`
		 where r.workspace_id = $1 and ($2 = '' or rp.key = $2)
		   and ($3 = '' or rp.key = $4 and r.number = $5 or r.title ilike '%' || $6 || '%')
		 order by rp.key = $4 and r.number = $5 desc, r.created_at desc, r.id desc limit 20`,
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

// escapeLike escapes LIKE's wildcards so s matches literally.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// checkRefs returns field errors for a status, assignee or labels that aren't the workspace's.
func (m *model) checkRefs(ctx context.Context, workspaceID string, f fields) (map[string]string, error) {
	var statusOK, assigneeOK bool
	var labels int
	err := m.db.QueryRow(ctx,
		`select $2::text is null or exists(select 1 from statuses where workspace_id = $1 and id::text = $2),
		        coalesce($3, '') = '' or exists(select 1 from workspace_members where workspace_id = $1 and user_id::text = $3),
		        (select count(*) from labels where workspace_id = $1 and id::text = any($4::text[]))`,
		workspaceID, f.statusID, f.assigneeID, f.labelIDs).Scan(&statusOK, &assigneeOK, &labels)
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
			`update projects set issue_seq = issue_seq + 1 where workspace_id = $1 and key = $2
			 returning id::text, issue_seq, archived_at is not null`, workspaceID, key).Scan(&projectID, &number, &archived); err != nil {
			return err
		}
		if archived {
			return errArchived
		}
		if err := tx.QueryRow(ctx,
			`insert into issues (workspace_id, project_id, number, title, description, status_id, assignee_id, priority, due_date, created_by)
			 values ($1, $2, $3, $4, coalesce($5, ''),
			         coalesce($6::uuid, (select id from statuses where workspace_id = $1 order by position, id limit 1)),
			         nullif($7, '')::uuid, coalesce($8::smallint, 0), nullif($9, '')::date, $10)
			 returning id::text`,
			workspaceID, projectID, number, f.title, f.description, f.statusID, f.assigneeID, f.priority, f.dueDate, userID).Scan(&id); err != nil {
			return err
		}
		if f.parent != nil {
			if err := setParent(ctx, tx, id, *f.parent); err != nil {
				return err
			}
		}
		return setLabels(ctx, tx, workspaceID, id, f.labelIDs)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if errors.Is(err, errArchived) || errors.As(err, new(parentError)) {
		return "", err
	}
	if err != nil {
		return "", fmt.Errorf("create issue: %w", err)
	}
	return id, nil
}

// update changes the issue's non-nil fields and sets updated_at.
func (m *model) update(ctx context.Context, workspaceID, id string, f fields) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if f.parent != nil {
			if err := setParent(ctx, tx, id, *f.parent); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx,
			`update issues set title = coalesce($2, title), description = coalesce($3, description),
			   status_id = coalesce($4::uuid, status_id),
			   assignee_id = case when $5::text is null then assignee_id else nullif($5, '')::uuid end,
			   priority = coalesce($6::smallint, priority),
			   due_date = case when $7::text is null then due_date else nullif($7, '')::date end,
			   updated_at = now()
			 where id::text = $1`,
			id, f.title, f.description, f.statusID, f.assigneeID, f.priority, f.dueDate); err != nil {
			return err
		}
		if !f.setLabels {
			return nil
		}
		if _, err := tx.Exec(ctx, `delete from issue_labels where issue_id::text = $1`, id); err != nil {
			return err
		}
		return setLabels(ctx, tx, workspaceID, id, f.labelIDs)
	})
	if errors.As(err, new(parentError)) {
		return err
	}
	if err != nil {
		return fmt.Errorf("update issue: %w", err)
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
			 where c.id = $1 and p.key = $2 and i.number = $3`, id, key, number).Scan(&parentID)
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

// link records that blocker blocks blocked. Returns errLinked when the two are already linked either way.
func (m *model) link(ctx context.Context, blockerID, blockedID string) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if err := lockIssues(ctx, tx, blockerID, blockedID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx,
			`insert into issue_links (blocker_id, blocked_id) select $1, $2
			 where not exists(select 1 from issue_links where blocker_id in ($1, $2) and blocked_id in ($1, $2))`,
			blockerID, blockedID)
		if err == nil && tag.RowsAffected() == 0 {
			return errLinked
		}
		return err
	})
	if errors.Is(err, errLinked) {
		return err
	}
	if err != nil {
		return fmt.Errorf("link issues: %w", err)
	}
	return nil
}

// unlink removes the link between the two issues, whichever way it goes; false when there's none.
func (m *model) unlink(ctx context.Context, a, b string) (bool, error) {
	tag, err := m.db.Exec(ctx,
		`delete from issue_links where blocker_id = $1 and blocked_id = $2 or blocker_id = $2 and blocked_id = $1`, a, b)
	if err != nil {
		return false, fmt.Errorf("unlink issues: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func setLabels(ctx context.Context, tx pgx.Tx, workspaceID, issueID string, labelIDs []string) error {
	if len(labelIDs) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx,
		`insert into issue_labels (issue_id, label_id)
		 select $2::uuid, id from labels where workspace_id = $1 and id::text = any($3::text[])`,
		workspaceID, issueID, labelIDs)
	return err
}

func (m *model) delete(ctx context.Context, id string) error {
	if _, err := m.db.Exec(ctx, `delete from issues where id::text = $1`, id); err != nil {
		return fmt.Errorf("delete issue: %w", err)
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

const statusCols = `id::text, name, category, position`

func scanStatus(row pgx.Row) (Status, error) {
	var s Status
	err := row.Scan(&s.ID, &s.Name, &s.Category, &s.Position)
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

// createStatus appends a status after the last one. Returns errNameTaken when the name is in use.
func (m *model) createStatus(ctx context.Context, workspaceID, name, category string) (Status, error) {
	s, err := scanStatus(m.db.QueryRow(ctx,
		`insert into statuses (workspace_id, name, category, position)
		 values ($1, $2, $3, (select coalesce(max(position) + 1, 0) from statuses where workspace_id = $1))
		 returning `+statusCols, workspaceID, name, category))
	if isUniqueViolation(err) {
		return s, errNameTaken
	}
	if err != nil {
		return s, fmt.Errorf("create status: %w", err)
	}
	return s, nil
}

// updateStatus renames and recategorizes a status; nil when there's none. Returns errNameTaken when the name is in use.
func (m *model) updateStatus(ctx context.Context, workspaceID, id, name, category string) (*Status, error) {
	s, err := scanStatus(m.db.QueryRow(ctx,
		`update statuses set name = $3, category = $4 where workspace_id = $1 and id::text = $2 returning `+statusCols,
		workspaceID, id, name, category))
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

// reorderStatuses sets positions to the order of ids. Returns errBadOrder unless ids is every status once.
func (m *model) reorderStatuses(ctx context.Context, workspaceID string, ids []string) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
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
			`update statuses s set position = o.ord - 1 from unnest($2::text[]) with ordinality o(id, ord)
			 where s.workspace_id = $1 and s.id::text = o.id`, workspaceID, ids)
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

// deleteStatus deletes a status, moving its issues to moveTo; false when there's no such status.
// Returns errLastStatus for the workspace's only status, and errInUse when issues use it and moveTo
// isn't another of the workspace's statuses.
func (m *model) deleteStatus(ctx context.Context, workspaceID, id, moveTo string) (bool, error) {
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
		if _, err := tx.Exec(ctx,
			`update issues i set status_id = s.id from statuses s
			 where i.status_id::text = $2 and s.workspace_id = $1 and s.id::text = $3 and s.id::text <> $2`,
			workspaceID, id, moveTo); err != nil {
			return err
		}
		var inUse bool
		if err := tx.QueryRow(ctx, `select exists(select 1 from issues where status_id::text = $1)`, id).Scan(&inUse); err != nil {
			return err
		}
		if inUse {
			return errInUse
		}
		_, err = tx.Exec(ctx, `delete from statuses where id::text = $1`, id)
		return err
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

// createLabel returns errNameTaken when the name is in use.
func (m *model) createLabel(ctx context.Context, workspaceID, name, color string) (Label, error) {
	l, err := scanLabel(m.db.QueryRow(ctx,
		`insert into labels (workspace_id, name, color) values ($1, $2, $3) returning `+labelCols, workspaceID, name, color))
	if isUniqueViolation(err) {
		return l, errNameTaken
	}
	if err != nil {
		return l, fmt.Errorf("create label: %w", err)
	}
	return l, nil
}

// updateLabel returns nil when there's no such label, and errNameTaken when the name is in use.
func (m *model) updateLabel(ctx context.Context, workspaceID, id, name, color string) (*Label, error) {
	l, err := scanLabel(m.db.QueryRow(ctx,
		`update labels set name = $3, color = $4 where workspace_id = $1 and id::text = $2 returning `+labelCols,
		workspaceID, id, name, color))
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

// deleteLabel deletes a label, which removes it from its issues; false when there's none.
func (m *model) deleteLabel(ctx context.Context, workspaceID, id string) (bool, error) {
	tag, err := m.db.Exec(ctx, `delete from labels where workspace_id = $1 and id::text = $2`, workspaceID, id)
	if err != nil {
		return false, fmt.Errorf("delete label: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func isUniqueViolation(err error) bool {
	pgErr := (*pgconn.PgError)(nil)
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
