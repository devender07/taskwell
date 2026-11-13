package template

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/issue"
	"github.com/devender07/taskwell/backend/user/internal/plan"
	"github.com/devender07/taskwell/backend/user/internal/realtime"
)

// fieldErrors are a form's field messages.
type fieldErrors map[string]string

func (fieldErrors) Error() string { return "invalid template" }

type ProjectRef struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

type Person struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Repeat struct {
	Every  int    `json:"every"`
	Unit   string `json:"unit"`
	NextOn string `json:"next_on"`
	Paused bool   `json:"paused"`
}

// Template is an issue template; Status and Labels are JSON as on an issue.
type Template struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Title       string          `json:"title"`
	Description json.RawMessage `json:"description"`
	Project     *ProjectRef     `json:"project"`
	Status      json.RawMessage `json:"status"`
	Assignee    *Person         `json:"assignee"`
	Priority    int             `json:"priority"`
	Labels      json.RawMessage `json:"labels"`
	Repeat      *Repeat         `json:"repeat"`
	CanEdit     bool            `json:"can_edit"`
}

// cols selects a Template from issue_templates t; a deleted label drops out.
const cols = `t.id::text, t.name, t.title, t.description,
	(select json_build_object('id', p.id, 'key', p.key, 'name', p.name) from projects p where p.id = t.project_id),
	(select status_json(s) from statuses s where s.id = t.status_id),
	(select json_build_object('id', u.id, 'name', u.name) from users u where u.id = t.assignee_id),
	t.priority,
	coalesce((select json_agg(json_build_object('id', l.id, 'name', l.name, 'color', l.color) order by lower(l.name), l.id)
	  from labels l where l.id = any(t.label_ids)), '[]'),
	t.repeat_every, t.repeat_unit, to_char(t.next_on, 'YYYY-MM-DD'), t.paused`

// seen is the condition that the viewer ($2) can see the template: it has no project or they can see that.
const seen = `(t.project_id is null or can_see_project(t.project_id, $2))`

func scan(row pgx.Row, canEdit bool) (Template, error) {
	t := Template{CanEdit: canEdit}
	var every *int
	var unit, nextOn *string
	var paused bool
	err := row.Scan(&t.ID, &t.Name, &t.Title, &t.Description, &t.Project, &t.Status, &t.Assignee, &t.Priority, &t.Labels,
		&every, &unit, &nextOn, &paused)
	if every != nil {
		t.Repeat = &Repeat{Every: *every, Unit: *unit, NextOn: *nextOn, Paused: paused}
	}
	return t, err
}

// model owns every query for templates.
type model struct{ db *pgxpool.Pool }

// list returns the workspace's templates the viewer can see, by name.
func (m *model) list(ctx context.Context, workspaceID, viewerID string, canEdit bool) ([]Template, error) {
	rows, err := m.db.Query(ctx, `select `+cols+` from issue_templates t where t.workspace_id = $1 and `+seen+`
		order by lower(t.name), t.id`, workspaceID, viewerID)
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}
	ts, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Template, error) { return scan(row, canEdit) })
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}
	return ts, nil
}

// changed tells the workspace its templates changed.
func changed(ctx context.Context, tx pgx.Tx, e realtime.Event) error {
	e.Kind, e.Paths = "template.changed", []string{realtime.Templates(e.Workspace)}
	return realtime.Publish(ctx, tx, e)
}

// save creates a template from the form (id "") or changes the fields the form sends on the workspace's template with
// the id (a UUID) that the actor can see, and publishes e; nil when there's no such template. Returns fieldErrors when
// the result isn't valid.
func (m *model) save(ctx context.Context, workspaceID, id, actorID string, form url.Values, e realtime.Event) (*Template, error) {
	var t *Template
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var v values
		if id != "" {
			err := tx.QueryRow(ctx, `select t.name, t.title, t.description, coalesce(p.key, ''), coalesce(t.status_id::text, ''),
				coalesce(t.assignee_id::text, ''), t.priority, array(select l.id::text from labels l where l.id = any(t.label_ids) order by l.id),
				coalesce(t.repeat_every, 0), coalesce(t.repeat_unit, ''), coalesce(to_char(t.next_on, 'YYYY-MM-DD'), ''), t.paused
				from issue_templates t left join projects p on p.id = t.project_id
				where t.workspace_id = $1 and `+seen+` and t.id = $3 for update of t`, workspaceID, actorID, id).
				Scan(&v.name, &v.title, &v.description, &v.projectKey, &v.statusID, &v.assigneeID, &v.priority, &v.labelIDs,
					&v.every, &v.unit, &v.nextOn, &v.paused)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
		}
		errs := apply(&v, form, utcToday())
		if v.labelIDs == nil {
			v.labelIDs = []string{}
		}
		projectID, err := checkRefs(ctx, tx, workspaceID, actorID, v, errs)
		if err != nil {
			return err
		}
		maps.DeleteFunc(errs, func(_, msg string) bool { return msg == "" })
		if len(errs) > 0 {
			return fieldErrors(errs)
		}
		var every, unit, nextOn any
		if v.every > 0 {
			every, unit, nextOn = v.every, v.unit, v.nextOn
		}
		args := []any{workspaceID, v.name, v.title, v.description, projectID, null(v.statusID), null(v.assigneeID), v.priority,
			v.labelIDs, every, unit, nextOn, v.paused}
		if id == "" {
			err = tx.QueryRow(ctx, `insert into issue_templates (workspace_id, name, title, description, project_id, status_id,
				assignee_id, priority, label_ids, repeat_every, repeat_unit, next_on, paused, created_by)
				values ($1, $2, $3, $4, $5, $6, $7, $8, $9::uuid[], $10, $11, $12::date, $13, $14) returning id::text`,
				append(args, actorID)...).Scan(&id)
		} else {
			_, err = tx.Exec(ctx, `update issue_templates set name = $2, title = $3, description = $4, project_id = $5,
				status_id = $6, assignee_id = $7, priority = $8, label_ids = $9::uuid[], repeat_every = $10, repeat_unit = $11,
				next_on = $12::date, paused = $13 where workspace_id = $1 and id = $14`, append(args, id)...)
		}
		if err != nil {
			return err
		}
		saved, err := scan(tx.QueryRow(ctx, `select `+cols+` from issue_templates t where t.id = $1`, id), true)
		if err != nil {
			return err
		}
		t = &saved
		return changed(ctx, tx, e)
	})
	if errors.As(err, new(fieldErrors)) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("save template: %w", err)
	}
	return t, nil
}

// checkRefs adds to errs when v's project isn't one the actor can see, or its status, assignee or labels aren't the
// workspace's, or the assignee can't see the project; fields errs already has are skipped. Returns the project's id
// (nil for none).
func checkRefs(ctx context.Context, tx pgx.Tx, workspaceID, actorID string, v values, errs map[string]string) (*string, error) {
	ref := func(field, id string) *string {
		if errs[field] != "" {
			return nil
		}
		return null(id)
	}
	var projectID *string
	var statusOK, memberOK, seesOK bool
	var labels int
	labelIDs := v.labelIDs
	if errs["label_ids"] != "" {
		labelIDs = nil
	}
	err := tx.QueryRow(ctx,
		`with p as (select id from projects where workspace_id = $1 and key = canonical_key($1, $2) and can_see_project(id, $3))
		 select (select id::text from p),
		        $4::uuid is null or exists(select 1 from statuses where workspace_id = $1 and id = $4),
		        $5::uuid is null or exists(select 1 from workspace_members where workspace_id = $1 and user_id = $5),
		        $5::uuid is null or coalesce((select can_see_project(p.id, $5) from p), true),
		        (select count(*) from labels where workspace_id = $1 and id = any($6::uuid[]))`,
		workspaceID, v.projectKey, actorID, ref("status_id", v.statusID), ref("assignee_id", v.assigneeID), labelIDs).
		Scan(&projectID, &statusOK, &memberOK, &seesOK, &labels)
	if err != nil {
		return nil, fmt.Errorf("check template refs: %w", err)
	}
	if v.projectKey != "" && projectID == nil && errs["project"] == "" {
		errs["project"] = "Choose a project."
	}
	if !statusOK {
		errs["status_id"] = errStatus
	}
	switch {
	case !memberOK:
		errs["assignee_id"] = errAssignee
	case !seesOK:
		errs["assignee_id"] = "Choose someone who can see this project."
	}
	if labels != len(labelIDs) {
		errs["label_ids"] = errLabels
	}
	return projectID, nil
}

// remove deletes the workspace's template with the id (a UUID) that the actor can see, and publishes e; false when
// there's none.
func (m *model) remove(ctx context.Context, workspaceID, id, actorID string, e realtime.Event) (bool, error) {
	found := false
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `delete from issue_templates t where t.workspace_id = $1 and `+seen+` and t.id = $3`,
			workspaceID, actorID, id)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		found = true
		return changed(ctx, tx, e)
	})
	if err != nil {
		return false, fmt.Errorf("delete template: %w", err)
	}
	return found, nil
}

// null is s, or nil (SQL null) when it's "".
func null(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// --- recurring ---

// due returns the ids of the templates that repeat, aren't paused and are due on or before today.
func (m *model) due(ctx context.Context, today time.Time, plans []string) ([]string, error) {
	rows, err := m.db.Query(ctx, `select id::text from issue_templates
		where repeat_every is not null and not paused and next_on <= $1
		  and ($2::text[] is null or `+plan.SQL("workspace_id")+` = any($2)) order by next_on, id`, today, plans)
	if err != nil {
		return nil, fmt.Errorf("due templates: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("due templates: %w", err)
	}
	return ids, nil
}

// occur creates the issue of the due template with the id and moves its next_on past today, or pauses it when its
// project is archived. It does nothing when the template is no longer due, or another instance holds it. Returns
// whether it created an issue.
func (m *model) occur(ctx context.Context, id string, today time.Time) (bool, error) {
	created := false
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var t issue.FromTemplate
		var wsID, slug, unit string
		var every int
		var on time.Time
		// The creator is who made it while they're still a member, else the workspace's first admin. An assignee who
		// can no longer see the project is dropped.
		err := tx.QueryRow(ctx, `select t.workspace_id::text, w.slug, t.project_id::text, t.title, t.description,
			coalesce(t.status_id::text, ''),
			case when exists(select 1 from workspace_members m where m.workspace_id = t.workspace_id and m.user_id = t.assignee_id)
			      and can_see_project(t.project_id, t.assignee_id) then t.assignee_id::text else '' end,
			t.priority, t.label_ids::text[], t.repeat_every, t.repeat_unit, t.next_on,
			coalesce((select t.created_by where exists(select 1 from workspace_members m
			            where m.workspace_id = t.workspace_id and m.user_id = t.created_by)),
			         (select m.user_id from workspace_members m join workspace_roles r on r.id = m.role_id
			            where m.workspace_id = t.workspace_id and r.builtin = 'admin' order by m.created_at, m.user_id limit 1))::text
			from issue_templates t join workspaces w on w.id = t.workspace_id
			where t.id = $1 and t.repeat_every is not null and not t.paused and t.next_on <= $2
			for update of t skip locked`, id, today).
			Scan(&wsID, &slug, &t.ProjectID, &t.Title, &t.Description, &t.StatusID, &t.AssigneeID, &t.Priority, &t.LabelIDs,
				&every, &unit, &on, &t.CreatorID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		e := realtime.Event{Workspace: slug, Audience: realtime.ToWorkspace(wsID)}
		var archived bool
		if err := tx.QueryRow(ctx, `select archived_at is not null from projects where id = $1 for no key update`,
			t.ProjectID).Scan(&archived); err != nil {
			return err
		}
		if archived {
			if _, err := tx.Exec(ctx, `update issue_templates set paused = true where id = $1`, id); err != nil {
				return err
			}
			return changed(ctx, tx, e)
		}
		t.Title += " · " + on.Format("Jan 2")
		if _, err := issue.CreateFromTemplate(ctx, tx, t); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `update issue_templates set next_on = $2 where id = $1`, id,
			upcoming(on, every, unit, today)); err != nil {
			return err
		}
		created = true
		return changed(ctx, tx, e)
	})
	if err != nil {
		return false, fmt.Errorf("template %s: %w", id, err)
	}
	return created, nil
}
