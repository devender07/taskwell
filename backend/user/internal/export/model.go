package export

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/doc"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

// Export is workspace.json: everything in the workspace the exporter can see. People are emails, null when gone.
type Export struct {
	ExportedAt time.Time `json:"exported_at"`
	ExportedBy struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"exported_by"`
	Workspace struct {
		Name      string    `json:"name"`
		Slug      string    `json:"slug"`
		CreatedAt time.Time `json:"created_at"`
	} `json:"workspace"`
	Members  []Member   `json:"members"`
	Roles    []Role     `json:"roles"`
	Statuses []Status   `json:"statuses"`
	Labels   []Label    `json:"labels"`
	Projects []Project  `json:"projects"`
	Issues   []Issue    `json:"issues"`
	Feedback []Feedback `json:"feedback"`
	Files    []File     `json:"files"`
}

type Member struct {
	Name     string    `json:"name"`
	Email    string    `json:"email"`
	Role     string    `json:"role"`
	Guest    bool      `json:"guest"`
	JoinedAt time.Time `json:"joined_at"`
}

type Role struct {
	Name        string   `json:"name"`
	Builtin     *string  `json:"builtin"`
	Permissions []string `json:"permissions"`
}

type Status struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	Position int    `json:"position"`
}

type Label struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type Project struct {
	Key         string  `json:"key"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Private     bool    `json:"private"`
	Archived    bool    `json:"archived"`
	StartDate   *string `json:"start_date"`
	TargetDate  *string `json:"target_date"`
	Milestones  []struct {
		Name string `json:"name"`
		Date string `json:"date"`
	} `json:"milestones"`
	Cycles []struct {
		Number   int     `json:"number"`
		Name     *string `json:"name"`
		StartsOn string  `json:"starts_on"`
		EndsOn   string  `json:"ends_on"`
	} `json:"cycles"`
	Updates []Update `json:"updates"`
}

type Update struct {
	Health    string          `json:"health"`
	BodyText  string          `json:"body_text"`
	Body      json.RawMessage `json:"body"`
	Author    *string         `json:"author"`
	CreatedAt time.Time       `json:"created_at"`
}

type Issue struct {
	ID              string          `json:"-"`
	Identifier      string          `json:"identifier"`
	Project         string          `json:"project"`
	Title           string          `json:"title"`
	DescriptionText string          `json:"description_text"`
	Description     json.RawMessage `json:"description"`
	Status          string          `json:"status"`
	Priority        string          `json:"priority"`
	Estimate        *int            `json:"estimate"`
	Assignee        *string         `json:"assignee"`
	CreatedBy       *string         `json:"created_by"`
	Labels          []string        `json:"labels"`
	DueDate         *string         `json:"due_date"`
	Cycle           *int            `json:"cycle"`
	Parent          *string         `json:"parent"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	Comments        []Comment       `json:"comments"`
	Files           []string        `json:"files"`
}

type Comment struct {
	Author    *string         `json:"author"`
	BodyText  string          `json:"body_text"`
	Body      json.RawMessage `json:"body"`
	CreatedAt time.Time       `json:"created_at"`
	EditedAt  *time.Time      `json:"edited_at"`
}

type Feedback struct {
	BodyText  string    `json:"body_text"`
	Name      *string   `json:"name"`
	Email     *string   `json:"email"`
	Source    string    `json:"source"`
	Status    string    `json:"status"`
	Issue     *string   `json:"issue"`
	CreatedAt time.Time `json:"created_at"`
}

// File is a project file or issue attachment. Path is where it sits in the zip, null when its bytes are gone.
type File struct {
	ID          string    `json:"-"`
	Name        string    `json:"name"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	Project     string    `json:"project"`
	Issue       *string   `json:"issue"`
	Folder      *string   `json:"folder"`
	UploadedBy  *string   `json:"uploaded_by"`
	CreatedAt   time.Time `json:"created_at"`
	Path        *string   `json:"path"`
}

// priorities names issues.priority 0–4 as the CSV import reads them.
var priorities = [...]string{"none", "urgent", "high", "medium", "low"}

type model struct{ db *pgxpool.Pool }

// slug returns the workspace's slug, "" when it's gone.
func (m *model) slug(ctx context.Context, wsID string) (string, error) {
	var slug string
	err := m.db.QueryRow(ctx, `select slug from workspaces where id = $1`, wsID).Scan(&slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("export workspace slug: %w", err)
	}
	return slug, nil
}

// visible keeps the projects p the user $2 can see in workspace $1.
const visible = `p.workspace_id = $1 and can_see_project(p.id, $2)`

// load reads everything in the workspace userID may see, in one transaction so the parts agree. Trashed issues are
// left out, and so are private projects the user isn't in, with their issues, comments and files.
// ponytail: the whole workspace sits in memory while the zip is written; stream issues per project if workspaces
// outgrow that.
func (m *model) load(ctx context.Context, wsID, userID string) (*Export, error) {
	var e Export
	err := pgx.BeginTxFunc(ctx, m.db, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `select now(), u.name, u.email, w.name, w.slug, w.created_at
			from workspaces w, users u where w.id = $1 and u.id = $2`, wsID, userID).
			Scan(&e.ExportedAt, &e.ExportedBy.Name, &e.ExportedBy.Email, &e.Workspace.Name, &e.Workspace.Slug, &e.Workspace.CreatedAt)
		if err != nil {
			return fmt.Errorf("export header: %w", err)
		}
		if e.Members, err = collect(ctx, tx, `select u.name, u.email, r.name, r.builtin is not distinct from 'guest', m.created_at
			from workspace_members m join users u on u.id = m.user_id join workspace_roles r on r.id = m.role_id
			where m.workspace_id = $1 order by m.created_at, u.email`, func(row pgx.Row, x *Member) error {
			err := row.Scan(&x.Name, &x.Email, &x.Role, &x.Guest, &x.JoinedAt)
			x.JoinedAt = x.JoinedAt.UTC()
			return err
		}, wsID); err != nil {
			return err
		}
		if e.Roles, err = collect(ctx, tx, `select name, builtin, permissions from workspace_roles
			where workspace_id = $1 order by created_at`, func(row pgx.Row, x *Role) error {
			err := row.Scan(&x.Name, &x.Builtin, &x.Permissions)
			if x.Builtin != nil {
				x.Permissions = workspace.Effective(*x.Builtin, x.Permissions)
			}
			return err
		}, wsID); err != nil {
			return err
		}
		if e.Statuses, err = collect(ctx, tx, `select name, category, position from statuses
			where workspace_id = $1 order by position`, func(row pgx.Row, x *Status) error {
			return row.Scan(&x.Name, &x.Category, &x.Position)
		}, wsID); err != nil {
			return err
		}
		if e.Labels, err = collect(ctx, tx, `select name, color from labels where workspace_id = $1 order by name`,
			func(row pgx.Row, x *Label) error { return row.Scan(&x.Name, &x.Color) }, wsID); err != nil {
			return err
		}
		if e.Projects, err = collect(ctx, tx, `select p.key, p.name, p.description, p.private, p.archived_at is not null,
			  p.start_date::text, p.target_date::text,
			  coalesce((select json_agg(json_build_object('name', x.name, 'date', x.date) order by x.date)
			            from project_milestones x where x.project_id = p.id), '[]'),
			  coalesce((select json_agg(json_build_object('number', c.number, 'name', c.name, 'starts_on', c.starts_on,
			              'ends_on', c.ends_on) order by c.number) from cycles c where c.project_id = p.id), '[]'),
			  coalesce((select json_agg(json_build_object('health', x.health, 'body', x.body, 'created_at', x.created_at,
			              'author', (select email from users where id = x.author_id)) order by x.created_at)
			            from project_updates x where x.project_id = p.id), '[]')
			from projects p where `+visible+` order by p.key`, func(row pgx.Row, x *Project) error {
			err := row.Scan(&x.Key, &x.Name, &x.Description, &x.Private, &x.Archived, &x.StartDate, &x.TargetDate,
				&x.Milestones, &x.Cycles, &x.Updates)
			for i := range x.Updates {
				u := &x.Updates[i]
				u.BodyText, u.CreatedAt = doc.PlainText(u.Body), u.CreatedAt.UTC()
			}
			return err
		}, wsID, userID); err != nil {
			return err
		}
		if e.Issues, err = collect(ctx, tx, `select i.id::text, p.key || '-' || i.number, p.key, i.title, i.description, s.name, i.priority, i.estimate, a.email, cb.email,
			  coalesce((select array_agg(l.name order by l.name) from issue_labels il join labels l on l.id = il.label_id
			            where il.issue_id = i.id), '{}'),
			  i.due_date::text, c.number, pp.key || '-' || pi.number, i.created_at, i.updated_at,
			  coalesce((select json_agg(json_build_object('body', x.body, 'created_at', x.created_at, 'edited_at', x.edited_at,
			              'author', (select email from users where id = x.author_id)) order by x.created_at)
			            from comments x where x.issue_id = i.id), '[]')
			from issues i join projects p on p.id = i.project_id and `+visible+`
			  join statuses s on s.id = i.status_id
			  left join users a on a.id = i.assignee_id left join users cb on cb.id = i.created_by
			  left join cycles c on c.id = i.cycle_id
			  left join issues pi on pi.id = i.parent_id left join projects pp on pp.id = pi.project_id
			where i.workspace_id = $1 order by p.key, i.number`, func(row pgx.Row, x *Issue) error {
			var priority int
			err := row.Scan(&x.ID, &x.Identifier, &x.Project, &x.Title, &x.Description, &x.Status,
				&priority, &x.Estimate, &x.Assignee, &x.CreatedBy, &x.Labels, &x.DueDate, &x.Cycle, &x.Parent, &x.CreatedAt, &x.UpdatedAt,
				&x.Comments)
			if priority >= 0 && priority < len(priorities) {
				x.Priority = priorities[priority]
			}
			x.DescriptionText, x.CreatedAt, x.UpdatedAt = doc.PlainText(x.Description), x.CreatedAt.UTC(), x.UpdatedAt.UTC()
			for i := range x.Comments {
				c := &x.Comments[i]
				c.BodyText, c.CreatedAt = doc.PlainText(c.Body), c.CreatedAt.UTC()
				if c.EditedAt != nil {
					*c.EditedAt = c.EditedAt.UTC()
				}
			}
			x.Files = []string{}
			return err
		}, wsID, userID); err != nil {
			return err
		}
		if e.Feedback, err = collect(ctx, tx, `select f.body, f.name, f.email, f.source,
			  case when f.archived_at is not null then 'archived' when f.issue_id is not null then 'linked' else 'new' end,
			  case when can_see_project(i.project_id, $2) then p.key || '-' || i.number end, f.created_at
			from feedback f left join issues i on i.id = f.issue_id left join projects p on p.id = i.project_id
			where f.workspace_id = $1 order by f.created_at`, func(row pgx.Row, x *Feedback) error {
			err := row.Scan(&x.BodyText, &x.Name, &x.Email, &x.Source, &x.Status, &x.Issue, &x.CreatedAt)
			x.CreatedAt = x.CreatedAt.UTC()
			return err
		}, wsID, userID); err != nil {
			return err
		}
		e.Files, err = collect(ctx, tx, `select f.id::text, f.name, f.content_type, f.size, p.key,
			  ip.key || '-' || i.number, d.name, u.email, f.created_at
			from files f join projects p on p.id = f.project_id and `+visible+`
			  left join issues i on i.id = f.issue_id left join projects ip on ip.id = i.project_id
			  left join file_folders d on d.id = f.folder_id left join users u on u.id = f.uploader_id
			where f.workspace_id = $1 and (f.issue_id is null or i.id is not null) order by f.created_at`,
			func(row pgx.Row, x *File) error {
				err := row.Scan(&x.ID, &x.Name, &x.ContentType, &x.Size, &x.Project, &x.Issue, &x.Folder, &x.UploadedBy, &x.CreatedAt)
				x.CreatedAt = x.CreatedAt.UTC()
				return err
			}, wsID, userID)
		return err
	})
	if err != nil {
		return nil, err
	}
	e.ExportedAt, e.Workspace.CreatedAt = e.ExportedAt.UTC(), e.Workspace.CreatedAt.UTC()
	return &e, nil
}

// collect runs sql and scans each row into a new T; never nil, so an empty list marshals as [].
func collect[T any](ctx context.Context, tx pgx.Tx, sql string, scan func(pgx.Row, *T) error, args ...any) ([]T, error) {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("export query: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (T, error) {
		var x T
		err := scan(row, &x)
		return x, err
	})
	if err != nil {
		return nil, fmt.Errorf("export rows: %w", err)
	}
	if out == nil {
		out = []T{}
	}
	return out, nil
}
