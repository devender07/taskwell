package projectfile

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/file"
	"github.com/devender07/taskwell/backend/user/internal/realtime"
)

var (
	errNoFolder  = errors.New("folder not found")
	errNameTaken = errors.New("folder name taken")
	errInside    = errors.New("folder inside itself")
	errNotEmpty  = errors.New("folder not empty")
)

// Person is who made a folder or uploaded a file; nil once their account is gone.
type Person struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Folder struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	ParentID  *string   `json:"parent_id"`
	CreatedBy *Person   `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	CanEdit   bool      `json:"can_edit"`
}

// File is a project's file or attachment. Only listed files (uploaded through Files) can be changed.
type File struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	Uploader    *Person   `json:"uploader"`
	CreatedAt   time.Time `json:"created_at"`
	URL         string    `json:"url"`
	CanEdit     bool      `json:"can_edit"`

	listed bool
}

// Crumb is one folder on the way from the root to the one listed.
type Crumb struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// viewer is who's asking: they can edit what they made, or anything with projects.manage.
type viewer struct {
	id       string
	manage   bool
	readOnly bool // the project is archived
}

func (v viewer) owns(p *Person) bool { return !v.readOnly && (v.manage || p != nil && p.ID == v.id) }

type project struct {
	id, key  string
	archived bool
}

type model struct{ db *pgxpool.Pool }

const (
	folderCols = `d.id::text, d.name, d.parent_id::text, u.id::text, u.name, d.created_at
		from file_folders d left join users u on u.id = d.created_by`
	fileCols = `f.id::text, f.name, f.content_type, f.size, u.id::text, u.name, f.created_at, f.listed
		from files f left join users u on u.id = f.uploader_id`
)

func person(id, name *string) *Person {
	if id == nil {
		return nil
	}
	return &Person{ID: *id, Name: *name}
}

func scanFolder(row pgx.Row, v viewer) (Folder, error) {
	var d Folder
	var uid, uname *string
	err := row.Scan(&d.ID, &d.Name, &d.ParentID, &uid, &uname, &d.CreatedAt)
	d.CreatedBy = person(uid, uname)
	d.CanEdit = v.owns(d.CreatedBy)
	return d, err
}

func scanFile(row pgx.Row, v viewer) (File, error) {
	var f File
	var uid, uname *string
	err := row.Scan(&f.ID, &f.Name, &f.ContentType, &f.Size, &uid, &uname, &f.CreatedAt, &f.listed)
	f.Uploader = person(uid, uname)
	f.URL = file.URL(f.ID, f.Name)
	f.CanEdit = v.owns(f.Uploader) // attachments: delete only
	return f, err
}

// project returns the workspace's project with the key (old keys too) when the viewer can see it, else nil.
func (m *model) project(ctx context.Context, workspaceID, key, viewerID string) (*project, error) {
	var p project
	err := m.db.QueryRow(ctx, `select id::text, key, archived_at is not null from projects where workspace_id = $1
		and key = canonical_key($1, $2) and can_see_project(id, $3)`, workspaceID, key, viewerID).Scan(&p.id, &p.key, &p.archived)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find project: %w", err)
	}
	return &p, nil
}

// Listings: what's directly in a folder (or the root), the attachments, or every name matching a search.
const (
	inFolder    = `d.parent_id is not distinct from $2::uuid`
	filesIn     = `f.listed and f.folder_id is not distinct from $2::uuid`
	attachments = `not f.listed and $2::text is null`
	issueFiles  = `f.issue_id = $2::uuid and exists (select 1 from issues i where i.id = f.issue_id)`
	noFolderFor = `false and $2::uuid is not null`
	noFolders   = `false and $2::text is null`
	nameLike    = `d.name ilike $2`
	fileLike    = `f.name ilike $2`
)

// list returns the project's folders matching folderWhere and files matching fileWhere (both constants above, with
// arg as $2): folders by name, files newest first.
// ponytail: no paging; add a cursor when a folder holds thousands of files.
func (m *model) list(ctx context.Context, projectID string, v viewer, folderWhere, fileWhere string, arg any) ([]Folder, []File, error) {
	rows, err := m.db.Query(ctx, `select `+folderCols+` where d.project_id = $1 and `+folderWhere+` order by lower(d.name), d.name`,
		projectID, arg)
	if err != nil {
		return nil, nil, fmt.Errorf("list folders: %w", err)
	}
	ds, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Folder, error) { return scanFolder(row, v) })
	if err != nil {
		return nil, nil, fmt.Errorf("list folders: %w", err)
	}
	rows, err = m.db.Query(ctx, `select `+fileCols+` where f.project_id = $1 and `+fileWhere+` order by f.created_at desc, f.id desc`,
		projectID, arg)
	if err != nil {
		return nil, nil, fmt.Errorf("list files: %w", err)
	}
	fs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (File, error) { return scanFile(row, v) })
	if err != nil {
		return nil, nil, fmt.Errorf("list files: %w", err)
	}
	return ds, fs, nil
}

// likePattern matches names containing q, its % and _ taken literally.
func likePattern(q string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
}

// path is the folders from the root down to the one with the id, itself included.
func (m *model) path(ctx context.Context, folderID string) ([]Crumb, error) {
	rows, err := m.db.Query(ctx, `with recursive up as (
		  select id, name, parent_id, 0 depth from file_folders where id = $1
		  union all select d.id, d.name, d.parent_id, up.depth + 1 from file_folders d join up on d.id = up.parent_id)
		select id::text, name from up order by depth desc`, folderID)
	if err != nil {
		return nil, fmt.Errorf("folder path: %w", err)
	}
	cs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Crumb, error) {
		var c Crumb
		err := row.Scan(&c.ID, &c.Name)
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("folder path: %w", err)
	}
	return cs, nil
}

// folder returns the project's folder with the id; nil when there's none.
func (m *model) folder(ctx context.Context, projectID, id string, v viewer) (*Folder, error) {
	d, err := scanFolder(m.db.QueryRow(ctx, `select `+folderCols+` where d.project_id = $1 and d.id = $2`, projectID, id), v)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find folder: %w", err)
	}
	return &d, nil
}

// file returns the project's file or attachment with the id; nil when there's none.
func (m *model) file(ctx context.Context, projectID, id string, v viewer) (*File, error) {
	f, err := scanFile(m.db.QueryRow(ctx, `select `+fileCols+` where f.project_id = $1 and f.id = $2`, projectID, id), v)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find file: %w", err)
	}
	return &f, nil
}

// hasFolder reports whether the project has a folder with the id.
func (m *model) hasFolder(ctx context.Context, projectID, id string) (bool, error) {
	var ok bool
	err := m.db.QueryRow(ctx, `select exists (select 1 from file_folders where project_id = $1 and id = $2)`, projectID, id).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("find folder: %w", err)
	}
	return ok, nil
}

// createFolder adds a folder in parentID (nil: the root) and publishes e; errNoFolder when the parent isn't the
// project's, errNameTaken when the name is.
func (m *model) createFolder(ctx context.Context, projectID string, parentID *string, name string, v viewer, e realtime.Event) (*Folder, error) {
	var d *Folder
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if err := checkParent(ctx, tx, projectID, parentID, ""); err != nil {
			return err
		}
		var id string
		if err := tx.QueryRow(ctx, `insert into file_folders (project_id, parent_id, name, created_by) values ($1, $2, $3, $4)
			returning id::text`, projectID, parentID, name, v.id).Scan(&id); err != nil {
			return err
		}
		f, err := scanFolder(tx.QueryRow(ctx, `select `+folderCols+` where d.id = $1`, id), v)
		d = &f
		if err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, e)
	})
	return d, folderErr("create folder", err)
}

// updateFolder renames the folder when name isn't nil and, when move is set, moves it into parentID (nil: the root),
// then publishes e. errInside when parentID is the folder or inside it.
func (m *model) updateFolder(ctx context.Context, projectID, id string, name *string, move bool, parentID *string, v viewer,
	e realtime.Event) (*Folder, error) {
	var d *Folder
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if move {
			// Moves take turns per project, so two crossing moves can't make a loop.
			if _, err := tx.Exec(ctx, `select 1 from projects where id = $1 for no key update`, projectID); err != nil {
				return err
			}
			if err := checkParent(ctx, tx, projectID, parentID, id); err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx, `update file_folders set name = coalesce($3, name), parent_id = case when $4 then $5 else parent_id end
			where id = $1 and project_id = $2`, id, projectID, name, move, parentID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errNoFolder
		}
		f, err := scanFolder(tx.QueryRow(ctx, `select `+folderCols+` where d.id = $1`, id), v)
		d = &f
		if err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, e)
	})
	return d, folderErr("update folder", err)
}

// checkParent returns errNoFolder when parentID (nil: the root) isn't one of the project's folders, and errInside
// when it is selfID or inside it.
func checkParent(ctx context.Context, tx pgx.Tx, projectID string, parentID *string, selfID string) error {
	if parentID == nil {
		return nil
	}
	var found, inside bool
	err := tx.QueryRow(ctx, `with recursive up as (
		  select id, parent_id from file_folders where id = $2 and project_id = $1
		  union all select d.id, d.parent_id from file_folders d join up on d.id = up.parent_id)
		select exists (select 1 from up), exists (select 1 from up where id = nullif($3, '')::uuid)`,
		projectID, *parentID, selfID).Scan(&found, &inside)
	switch {
	case err != nil:
		return err
	case !found:
		return errNoFolder
	case inside:
		return errInside
	}
	return nil
}

// deleteFolder removes the project's empty folder with the id and publishes e; errNotEmpty when it holds anything.
func (m *model) deleteFolder(ctx context.Context, projectID, id string, e realtime.Event) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `delete from file_folders d where d.id = $1 and d.project_id = $2
			and not exists (select 1 from file_folders c where c.parent_id = d.id)
			and not exists (select 1 from files f where f.folder_id = d.id)`, id, projectID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errNotEmpty
		}
		return realtime.Publish(ctx, tx, e)
	})
	return folderErr("delete folder", err)
}

// updateFile renames the listed file when name isn't nil and, when move is set, moves it into folderID (nil: the
// root), then publishes e. errNoFolder when the folder isn't the project's.
func (m *model) updateFile(ctx context.Context, projectID, id string, name *string, move bool, folderID *string, v viewer,
	e realtime.Event) (*File, error) {
	var f *File
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if move {
			if err := checkParent(ctx, tx, projectID, folderID, ""); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `update files set name = coalesce($3, name), folder_id = case when $4 then $5 else folder_id end
			where id = $1 and project_id = $2 and listed`, id, projectID, name, move, folderID); err != nil {
			return err
		}
		got, err := scanFile(tx.QueryRow(ctx, `select `+fileCols+` where f.id = $1`, id), v)
		f = &got
		if err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, e)
	})
	return f, folderErr("update file", err)
}

// deleteFile removes the project's file or attachment with the id and publishes e; false when there was none.
func (m *model) deleteFile(ctx context.Context, projectID, id string, e realtime.Event) (bool, error) {
	var ok bool
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `delete from files where id = $1 and project_id = $2`, id, projectID)
		if err != nil {
			return err
		}
		if ok = tag.RowsAffected() > 0; !ok {
			return nil
		}
		return realtime.Publish(ctx, tx, e)
	})
	if err != nil {
		return false, fmt.Errorf("delete file: %w", err)
	}
	return ok, nil
}

// folderErr maps constraint violations to the errors the controller answers, passes those through, and wraps the
// rest with op.
func folderErr(op string, err error) error {
	var pe *pgconn.PgError
	switch {
	case err == nil, errors.Is(err, errNoFolder), errors.Is(err, errInside), errors.Is(err, errNotEmpty):
		return err
	case errors.As(err, &pe) && pe.Code == "23505":
		return errNameTaken
	case errors.As(err, &pe) && pe.Code == "23503" && pe.ConstraintName == "files_folder_id_fkey":
		return errNotEmpty
	case errors.As(err, &pe) && pe.Code == "23503":
		return errNoFolder
	}
	return fmt.Errorf("%s: %w", op, err)
}
