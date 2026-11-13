package defaults

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/devender07/taskwell/backend/admin/internal/audit"
	"github.com/devender07/taskwell/backend/admin/internal/realtime"
	"github.com/devender07/taskwell/backend/shared/perms"
)

type Role struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Builtin     *string  `json:"builtin"`
	Permissions []string `json:"permissions"`
}

type StatusType struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Category string  `json:"category"`
	Icon     string  `json:"icon"`
	Color    *string `json:"color"`
	Position int     `json:"position"`
}

type Status struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Category string  `json:"category"`
	TypeID   *string `json:"type_id"`
	Position int     `json:"position"`
}

type Label struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type Template struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Title       string          `json:"title"`
	Description json.RawMessage `json:"description"`
	StatusID    *string         `json:"status_id"`
	Priority    int             `json:"priority"`
	LabelIDs    []string        `json:"label_ids"`
}

// Defaults is GET /api/defaults.
type Defaults struct {
	Permissions []perms.Permission `json:"permissions"`
	Roles       []Role             `json:"roles"`
	StatusTypes []StatusType       `json:"status_types"`
	Statuses    []Status           `json:"statuses"`
	Labels      []Label            `json:"labels"`
	Templates   []Template         `json:"templates"`
}

var (
	errNotFound   = errors.New("not found")
	errNameTaken  = errors.New("name taken")
	errBuiltin    = errors.New("built-in role")
	errLastStatus = errors.New("last status")
	errBadOrder   = errors.New("order must list every status once")
	errNoType     = errors.New("no such type")
	errNoStatus   = errors.New("no such status")
	errNoLabel    = errors.New("no such label")
)

// conn is the pool, or a transaction in tests (writes then run in savepoints and roll back with it).
type conn interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// model owns every default_* query. Ids are compared as text so a malformed one is just not found.
type model struct{ db conn }

const (
	roleCols     = `id::text, name, builtin, permissions`
	typeCols     = `id::text, name, category, icon, color, position`
	statusCols   = `id::text, name, category, type_id::text, position`
	labelCols    = `id::text, name, color`
	templateCols = `id::text, name, title, description, status_id::text, priority, array(select l::text from unnest(label_ids) l)`
)

func scanRole(row pgx.Row) (r Role, err error) {
	return r, row.Scan(&r.ID, &r.Name, &r.Builtin, &r.Permissions)
}

func scanType(row pgx.Row) (t StatusType, err error) {
	return t, row.Scan(&t.ID, &t.Name, &t.Category, &t.Icon, &t.Color, &t.Position)
}

func scanStatus(row pgx.Row) (s Status, err error) {
	return s, row.Scan(&s.ID, &s.Name, &s.Category, &s.TypeID, &s.Position)
}

func scanLabel(row pgx.Row) (l Label, err error) {
	return l, row.Scan(&l.ID, &l.Name, &l.Color)
}

func scanTemplate(row pgx.Row) (t Template, err error) {
	return t, row.Scan(&t.ID, &t.Name, &t.Title, &t.Description, &t.StatusID, &t.Priority, &t.LabelIDs)
}

func collect[T any](ctx context.Context, q conn, scan func(pgx.Row) (T, error), sql string, args ...any) ([]T, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (T, error) { return scan(row) })
}

func (m *model) all(ctx context.Context) (*Defaults, error) {
	d := Defaults{Permissions: perms.Active()}
	var err error
	if d.Roles, err = collect(ctx, m.db, scanRole, `select `+roleCols+` from default_roles
		order by case builtin when 'member' then 0 when 'guest' then 1 else 2 end, lower(name), id`); err != nil {
		return nil, fmt.Errorf("list default roles: %w", err)
	}
	if d.StatusTypes, err = collect(ctx, m.db, scanType, `select `+typeCols+` from default_status_types order by position, id`); err != nil {
		return nil, fmt.Errorf("list default status types: %w", err)
	}
	if d.Statuses, err = m.statuses(ctx, m.db); err != nil {
		return nil, err
	}
	if d.Labels, err = collect(ctx, m.db, scanLabel, `select `+labelCols+` from default_labels order by lower(name), id`); err != nil {
		return nil, fmt.Errorf("list default labels: %w", err)
	}
	if d.Templates, err = collect(ctx, m.db, scanTemplate, `select `+templateCols+` from default_templates order by lower(name), id`); err != nil {
		return nil, fmt.Errorf("list default templates: %w", err)
	}
	return &d, nil
}

func (m *model) statuses(ctx context.Context, q conn) ([]Status, error) {
	s, err := collect(ctx, q, scanStatus, `select `+statusCols+` from default_statuses order by position, id`)
	if err != nil {
		return nil, fmt.Errorf("list default statuses: %w", err)
	}
	return s, nil
}

// change runs fn in a transaction that records the admin's defaults.update of target (roles, statuses, …) and tells
// admin tabs /defaults changed. A pgx.ErrNoRows from fn is errNotFound; a unique violation is errNameTaken.
func (m *model) change(ctx context.Context, adminID, ip, target string, fn func(tx pgx.Tx) error) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if err := fn(tx); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, adminID, audit.DefaultsUpdate, target, ip); err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, "admin.defaults", adminID, "/defaults")
	})
	var pgErr *pgconn.PgError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pgx.ErrNoRows):
		return errNotFound
	case errors.As(err, &pgErr) && pgErr.Code == "23505":
		return errNameTaken
	case errors.Is(err, errBuiltin), errors.Is(err, errLastStatus), errors.Is(err, errBadOrder),
		errors.Is(err, errNoType), errors.Is(err, errNoStatus), errors.Is(err, errNoLabel):
		return err
	}
	return fmt.Errorf("update %s defaults: %w", target, err)
}

// --- roles ---

// role returns the role, or nil when there's none.
func (m *model) role(ctx context.Context, id string) (*Role, error) {
	r, err := scanRole(m.db.QueryRow(ctx, `select `+roleCols+` from default_roles where id::text = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read default role: %w", err)
	}
	return &r, nil
}

// saveRole creates a custom role (id "") or updates one; the name of a built-in role stays.
func (m *model) saveRole(ctx context.Context, adminID, ip, id, name string, keys []string) (r Role, err error) {
	return r, m.change(ctx, adminID, ip, "roles", func(tx pgx.Tx) error {
		if id == "" {
			r, err = scanRole(tx.QueryRow(ctx,
				`insert into default_roles (name, permissions) values ($1, $2) returning `+roleCols, name, keys))
			return err
		}
		r, err = scanRole(tx.QueryRow(ctx, `update default_roles set name = case when builtin is null then $2 else name end,
			permissions = $3 where id::text = $1 returning `+roleCols, id, name, keys))
		return err
	})
}

func (m *model) deleteRole(ctx context.Context, adminID, ip, id string) error {
	return m.change(ctx, adminID, ip, "roles", func(tx pgx.Tx) error {
		var builtin *string
		if err := tx.QueryRow(ctx, `delete from default_roles where id::text = $1 returning builtin`, id).Scan(&builtin); err != nil {
			return err
		}
		if builtin != nil {
			return errBuiltin // rolls the delete back
		}
		return nil
	})
}

// --- status types ---

func (m *model) saveType(ctx context.Context, adminID, ip, id, name, category, icon string, color *string) (t StatusType, err error) {
	return t, m.change(ctx, adminID, ip, "status_types", func(tx pgx.Tx) error {
		if id == "" {
			t, err = scanType(tx.QueryRow(ctx, `insert into default_status_types (name, category, icon, color, position)
				values ($1, $2, $3, $4, (select coalesce(max(position) + 1, 0) from default_status_types)) returning `+typeCols,
				name, category, icon, color))
			return err
		}
		if t, err = scanType(tx.QueryRow(ctx, `update default_status_types set name = $2, category = $3, icon = $4, color = $5
			where id::text = $1 returning `+typeCols, id, name, category, icon, color)); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `update default_statuses set category = $2 where type_id::text = $1 and category <> $2`, id, category)
		return err
	})
}

// deleteType removes the type; its statuses keep their category, without a type.
func (m *model) deleteType(ctx context.Context, adminID, ip, id string) error {
	return m.change(ctx, adminID, ip, "status_types", func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `delete from default_status_types where id::text = $1 returning id`, id).Scan(new(string))
	})
}

// --- statuses ---

// typeCategory is the category of the type, or category when there's no type; errNoType when it doesn't exist.
func typeCategory(ctx context.Context, tx pgx.Tx, typeID *string, category string) (string, error) {
	if typeID == nil {
		return category, nil
	}
	err := tx.QueryRow(ctx, `select category from default_status_types where id::text = $1 for share`, *typeID).Scan(&category)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errNoType
	}
	return category, err
}

// saveStatus creates a status after the last one (id "") or updates one. With a type, the category is the type's.
func (m *model) saveStatus(ctx context.Context, adminID, ip, id, name, category string, typeID *string) (s Status, err error) {
	return s, m.change(ctx, adminID, ip, "statuses", func(tx pgx.Tx) error {
		if category, err = typeCategory(ctx, tx, typeID, category); err != nil {
			return err
		}
		if id == "" {
			s, err = scanStatus(tx.QueryRow(ctx, `insert into default_statuses (name, category, type_id, position)
				values ($1, $2, $3::text::uuid, (select coalesce(max(position) + 1, 0) from default_statuses)) returning `+statusCols,
				name, category, typeID))
			return err
		}
		s, err = scanStatus(tx.QueryRow(ctx, `update default_statuses set name = $2, category = $3, type_id = $4::text::uuid
			where id::text = $1 returning `+statusCols, id, name, category, typeID))
		return err
	})
}

// lockStatuses returns the status ids, locking them against concurrent reorders and deletes.
func lockStatuses(ctx context.Context, tx pgx.Tx) ([]string, error) {
	rows, err := tx.Query(ctx, `select id::text from default_statuses for update`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// deleteStatus returns errLastStatus for the only one.
func (m *model) deleteStatus(ctx context.Context, adminID, ip, id string) error {
	return m.change(ctx, adminID, ip, "statuses", func(tx pgx.Tx) error {
		have, err := lockStatuses(ctx, tx)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `delete from default_statuses where id::text = $1 returning id`, id).Scan(new(string)); err != nil {
			return err
		}
		if len(have) <= 1 {
			return errLastStatus
		}
		return nil
	})
}

// reorderStatuses sets positions to the order of ids and returns the statuses; errBadOrder unless ids is every
// status once.
func (m *model) reorderStatuses(ctx context.Context, adminID, ip string, ids []string) (out []Status, err error) {
	return out, m.change(ctx, adminID, ip, "statuses", func(tx pgx.Tx) error {
		have, err := lockStatuses(ctx, tx)
		if err != nil {
			return err
		}
		if !sameSet(have, ids) {
			return errBadOrder
		}
		if _, err := tx.Exec(ctx, `update default_statuses s set position = o.ord - 1
			from unnest($1::text[]) with ordinality o(id, ord) where s.id::text = o.id`, ids); err != nil {
			return err
		}
		out, err = m.statuses(ctx, tx)
		return err
	})
}

// --- labels ---

func (m *model) saveLabel(ctx context.Context, adminID, ip, id, name, color string) (l Label, err error) {
	return l, m.change(ctx, adminID, ip, "labels", func(tx pgx.Tx) error {
		if id == "" {
			l, err = scanLabel(tx.QueryRow(ctx, `insert into default_labels (name, color) values ($1, $2) returning `+labelCols, name, color))
			return err
		}
		l, err = scanLabel(tx.QueryRow(ctx,
			`update default_labels set name = $2, color = $3 where id::text = $1 returning `+labelCols, id, name, color))
		return err
	})
}

// deleteLabel removes the label and takes it off the templates.
func (m *model) deleteLabel(ctx context.Context, adminID, ip, id string) error {
	return m.change(ctx, adminID, ip, "labels", func(tx pgx.Tx) error {
		var uuid string
		if err := tx.QueryRow(ctx, `delete from default_labels where id::text = $1 returning id::text`, id).Scan(&uuid); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `update default_templates set label_ids = array_remove(label_ids, $1::uuid) where $1::uuid = any(label_ids)`, uuid)
		return err
	})
}

// --- templates ---

type templateForm struct {
	name, title string
	description json.RawMessage // nil: none
	statusID    *string
	priority    int
	labelIDs    []string // distinct
}

// saveTemplate creates a template (id "") or updates one; errNoStatus or errNoLabel when a reference doesn't exist.
func (m *model) saveTemplate(ctx context.Context, adminID, ip, id string, f templateForm) (t Template, err error) {
	return t, m.change(ctx, adminID, ip, "templates", func(tx pgx.Tx) error {
		var statusOK bool
		var labels int
		if err := tx.QueryRow(ctx, `select $1::text is null or exists (select 1 from default_statuses where id::text = $1 for share),
			(select count(*) from (select 1 from default_labels where id::text = any($2::text[]) for share) l)`,
			f.statusID, f.labelIDs).Scan(&statusOK, &labels); err != nil {
			return err
		}
		switch {
		case !statusOK:
			return errNoStatus
		case labels != len(f.labelIDs):
			return errNoLabel
		}
		var desc any // nil stores SQL null
		if f.description != nil {
			desc = string(f.description)
		}
		if id == "" {
			t, err = scanTemplate(tx.QueryRow(ctx, `insert into default_templates (name, title, description, status_id, priority, label_ids)
				values ($1, $2, $3::jsonb, $4::text::uuid, $5, $6::text[]::uuid[]) returning `+templateCols,
				f.name, f.title, desc, f.statusID, f.priority, f.labelIDs))
			return err
		}
		t, err = scanTemplate(tx.QueryRow(ctx, `update default_templates set name = $2, title = $3, description = $4::jsonb,
			status_id = $5::text::uuid, priority = $6, label_ids = $7::text[]::uuid[] where id::text = $1 returning `+templateCols,
			id, f.name, f.title, desc, f.statusID, f.priority, f.labelIDs))
		return err
	})
}

func (m *model) deleteTemplate(ctx context.Context, adminID, ip, id string) error {
	return m.change(ctx, adminID, ip, "templates", func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `delete from default_templates where id::text = $1 returning id`, id).Scan(new(string))
	})
}
