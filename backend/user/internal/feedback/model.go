package feedback

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

// pageSize caps a page of the inbox.
const pageSize = 50

// Feedback is one customer's feedback. Issue is null when unlinked or when the viewer can't see the linked issue.
type Feedback struct {
	ID        string          `json:"id"`
	Body      string          `json:"body"`
	Email     *string         `json:"email"`
	Name      *string         `json:"name"`
	Source    string          `json:"source"`
	Issue     json.RawMessage `json:"issue"`
	Archived  bool            `json:"archived"`
	CreatedBy json.RawMessage `json:"created_by"`
	CreatedAt time.Time       `json:"created_at"`
}

// scope is who changes the workspace's feedback; userID is "" for the public form.
type scope struct{ wsID, slug, userID string }

// issueRef is a linked issue; zero when there's none.
type issueRef struct{ id, identifier, projectID string }

type model struct{ db *pgxpool.Pool }

// cols selects a Feedback from feedback f in workspace $1 as seen by user $2.
const cols = `select f.id::text, f.body, f.email, f.name, f.source,
	  case when i.id is not null and can_see_project(i.project_id, $2) then
	    json_build_object('identifier', p.key || '-' || i.number, 'title', i.title, 'status', status_json(s)) end,
	  f.archived_at is not null,
	  (select json_build_object('id', u.id, 'name', u.name) from users u where u.id = f.created_by),
	  f.created_at
	from feedback f left join issues i on i.id = f.issue_id left join projects p on p.id = i.project_id
	  left join statuses s on s.id = i.status_id
	where f.workspace_id = $1`

func (m *model) query(ctx context.Context, sql string, args ...any) ([]Feedback, error) {
	rows, err := m.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Feedback, error) {
		var f Feedback
		err := row.Scan(&f.ID, &f.Body, &f.Email, &f.Name, &f.Source, &f.Issue, &f.Archived, &f.CreatedBy, &f.CreatedAt)
		f.CreatedAt = f.CreatedAt.UTC()
		return f, err
	})
}

// list returns a page of the state's feedback matching q, newest first, older than before (from the newest when nil),
// and whether there's more.
func (m *model) list(ctx context.Context, wsID, viewerID, state, q string, before *string) ([]Feedback, bool, error) {
	fs, err := m.query(ctx, cols+`
		and case $3 when 'linked' then f.archived_at is null and f.issue_id is not null
		            when 'archived' then f.archived_at is not null
		            else f.archived_at is null and f.issue_id is null end
		and ($4 = '' or f.body ilike '%' || $4 || '%' or f.email ilike '%' || $4 || '%' or f.name ilike '%' || $4 || '%')
		and ($5::uuid is null or f.id < $5)
		order by f.id desc limit $6`, wsID, viewerID, state, q, before, pageSize+1)
	if err != nil {
		return nil, false, fmt.Errorf("list feedback: %w", err)
	}
	if len(fs) > pageSize {
		return fs[:pageSize], true, nil
	}
	return fs, false, nil
}

// get returns the feedback, or nil when it isn't in the workspace.
func (m *model) get(ctx context.Context, wsID, viewerID, id string) (*Feedback, error) {
	fs, err := m.query(ctx, cols+` and f.id = $3`, wsID, viewerID, id)
	if err != nil || len(fs) == 0 {
		return nil, err
	}
	return &fs[0], nil
}

// forIssue returns a page of the issue's feedback, newest first, archived included, older than before (from the
// newest when nil), whether there's more and how many it has in all.
func (m *model) forIssue(ctx context.Context, wsID, viewerID, issueID string, before *string) ([]Feedback, bool, int, error) {
	fs, err := m.query(ctx, cols+` and f.issue_id = $3 and ($4::uuid is null or f.id < $4) order by f.id desc limit $5`,
		wsID, viewerID, issueID, before, pageSize+1)
	if err != nil {
		return nil, false, 0, fmt.Errorf("list issue feedback: %w", err)
	}
	var total int
	if err := m.db.QueryRow(ctx, `select count(*) from feedback where workspace_id = $1 and issue_id = $2`, wsID, issueID).
		Scan(&total); err != nil {
		return nil, false, 0, fmt.Errorf("count issue feedback: %w", err)
	}
	if len(fs) > pageSize {
		return fs[:pageSize], true, total, nil
	}
	return fs, false, total, nil
}

func (m *model) countNew(ctx context.Context, wsID string) (int, error) {
	var n int
	err := m.db.QueryRow(ctx, `select count(*) from feedback where workspace_id = $1 and archived_at is null and issue_id is null`,
		wsID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count feedback: %w", err)
	}
	return n, nil
}

// issue resolves the identifier's key and number to an issue the viewer can see; nil when there's none.
func (m *model) issue(ctx context.Context, wsID, viewerID, key string, number int) (*issueRef, error) {
	var ref issueRef
	err := m.db.QueryRow(ctx,
		`select i.id::text, p.key || '-' || i.number, p.id::text from issues i join projects p on p.id = i.project_id
		 where i.workspace_id = $1 and p.key = canonical_key($1, $2) and i.number = $3 and can_see_project(p.id, $4)`,
		wsID, key, number, viewerID).Scan(&ref.id, &ref.identifier, &ref.projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find issue: %w", err)
	}
	return &ref, nil
}

// create adds feedback from source ("form" or "member") and returns its id.
func (m *model) create(ctx context.Context, sc scope, in input, source string) (string, error) {
	var id string
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			`insert into feedback (workspace_id, body, email, name, source, created_by) values ($1, $2, $3, $4, $5, nullif($6, '')::uuid)
			 returning id::text`, sc.wsID, in.Body, in.Email, in.Name, source, sc.userID).Scan(&id); err != nil {
			return err
		}
		return changed(ctx, tx, sc)
	})
	if err != nil {
		return "", fmt.Errorf("create feedback: %w", err)
	}
	return id, nil
}

// linkedSQL selects the issue linked to feedback d: its id, identifier and project, nulls when there's none.
const linkedSQL = `select i.id::text, p.key || '-' || i.number, p.id::text
	from d left join issues i on i.id = d.issue_id left join projects p on p.id = i.project_id`

func scanRef(row pgx.Row) (issueRef, error) {
	var id, identifier, projectID *string
	err := row.Scan(&id, &identifier, &projectID)
	if id == nil {
		return issueRef{}, err
	}
	return issueRef{*id, *identifier, *projectID}, err
}

// update links the feedback to link (unlinks when it's the zero issueRef) when setIssue, and archives or unarchives it
// when archived isn't nil. It reports whether the feedback is in the workspace.
func (m *model) update(ctx context.Context, sc scope, id string, setIssue bool, link issueRef, archived *bool) (bool, error) {
	found := true
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		old, err := scanRef(tx.QueryRow(ctx, `with d as (select issue_id from feedback where id = $1 and workspace_id = $2 for update) `+
			linkedSQL, id, sc.wsID))
		if errors.Is(err, pgx.ErrNoRows) {
			found = false
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`update feedback set issue_id = case when $2 then nullif($3, '')::uuid else issue_id end,
			   archived_at = case when $4::bool is null then archived_at when $4 then coalesce(archived_at, now()) end
			 where id = $1`, id, setIssue, link.id, archived); err != nil {
			return err
		}
		if !setIssue || link.id == old.id {
			link = issueRef{}
		}
		return changed(ctx, tx, sc, old, link)
	})
	if err != nil {
		return false, fmt.Errorf("update feedback: %w", err)
	}
	return found, nil
}

// delete removes the feedback and reports whether it was in the workspace.
func (m *model) delete(ctx context.Context, sc scope, id string) (bool, error) {
	found := true
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		old, err := scanRef(tx.QueryRow(ctx, `with d as (delete from feedback where id = $1 and workspace_id = $2 returning issue_id) `+
			linkedSQL, id, sc.wsID))
		if errors.Is(err, pgx.ErrNoRows) {
			found = false
			return nil
		}
		if err != nil {
			return err
		}
		return changed(ctx, tx, sc, old)
	})
	if err != nil {
		return false, fmt.Errorf("delete feedback: %w", err)
	}
	return found, nil
}

// formToken is the workspace's public form token, nil while the form is off.
func (m *model) formToken(ctx context.Context, wsID string) (*string, error) {
	var token *string
	if err := m.db.QueryRow(ctx, `select feedback_token from workspaces where id = $1`, wsID).Scan(&token); err != nil {
		return nil, fmt.Errorf("feedback form: %w", err)
	}
	return token, nil
}

// setForm turns the form on or off (enabled nil keeps it as it is) and, when it's on, sets token if it had none or
// rotate is set. It returns the form's token, nil when it's off.
func (m *model) setForm(ctx context.Context, wsID string, enabled *bool, rotate bool, token string) (*string, error) {
	var out *string
	err := m.db.QueryRow(ctx,
		`update workspaces set feedback_token = case
		   when not coalesce($2::bool, feedback_token is not null) then null
		   when $3 or feedback_token is null then $4
		   else feedback_token end
		 where id = $1 returning feedback_token`, wsID, enabled, rotate, token).Scan(&out)
	if err != nil {
		return nil, fmt.Errorf("set feedback form: %w", err)
	}
	return out, nil
}

// formWorkspace returns the scope and name of the workspace whose form is on with the token; ok=false when none is.
func (m *model) formWorkspace(ctx context.Context, token string) (sc scope, name string, ok bool, err error) {
	err = m.db.QueryRow(ctx, `select id::text, slug, name from workspaces where feedback_token = $1`, token).
		Scan(&sc.wsID, &sc.slug, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return sc, "", false, nil
	}
	if err != nil {
		return sc, "", false, fmt.Errorf("find feedback form: %w", err)
	}
	return sc, name, true, nil
}

// changed tells the workspace its feedback list and count changed, and the people who can see each issue that its
// feedback did.
func changed(ctx context.Context, tx pgx.Tx, sc scope, issues ...issueRef) error {
	e := realtime.Event{Kind: "feedback.changed", Workspace: sc.slug, Actor: sc.userID, Audience: realtime.ToWorkspace(sc.wsID),
		Paths: []string{realtime.Feedback(sc.slug), realtime.Feedback(sc.slug) + "/count"}}
	if err := realtime.Publish(ctx, tx, e); err != nil {
		return err
	}
	for _, i := range issues {
		if i.id == "" {
			continue
		}
		ie := e
		ie.Project, ie.Paths = i.projectID, []string{realtime.Issue(sc.slug, i.identifier) + "/feedback"}
		if err := realtime.Publish(ctx, tx, ie); err != nil {
			return err
		}
	}
	return nil
}
