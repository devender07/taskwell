package file

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/realtime"
)

type model struct{ db *pgxpool.Pool }

// File is an uploaded file as POST /api/workspaces/{slug}/files returns it.
type File struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	Image       bool   `json:"image"`

	workspaceID *string
	uploaderID  string
	place       *Placement
}

// Placement puts an upload in a project: in its Files (Listed, in FolderID or the root) or as an attachment.
// Event is published when the upload commits.
type Placement struct {
	ProjectID string
	FolderID  *string
	IssueID   *string // attached to this issue, listed under its description
	Listed    bool
	Event     realtime.Event
}

// create inserts the file's row and calls store with the new id inside the same transaction, so a row only
// commits once its bytes are stored.
func (m *model) create(ctx context.Context, f *File, store func(id string) error) error {
	pl := f.place
	if pl == nil {
		pl = &Placement{}
	}
	return pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx,
			`insert into files (workspace_id, uploader_id, name, content_type, size, project_id, folder_id, listed, issue_id)
			 values ($1, $2, $3, $4, $5, nullif($6, '')::uuid, $7, $8, $9) returning id::text`,
			f.workspaceID, f.uploaderID, f.Name, f.ContentType, f.Size, pl.ProjectID, pl.FolderID, pl.Listed, pl.IssueID).Scan(&f.ID)
		if err != nil {
			return fmt.Errorf("insert file: %w", err)
		}
		if pl.ProjectID != "" {
			if err := realtime.Publish(ctx, tx, pl.Event); err != nil {
				return err
			}
		}
		return store(f.ID)
	})
}

// used returns the bytes the workspace's files take up.
func (m *model) used(ctx context.Context, workspaceID string) (int64, error) {
	var n int64
	if err := m.db.QueryRow(ctx, `select coalesce(sum(size), 0) from files where workspace_id = $1`, workspaceID).Scan(&n); err != nil {
		return 0, fmt.Errorf("sum file sizes: %w", err)
	}
	return n, nil
}

// project returns the id and current key of the workspace's project with the key (old keys too) when the viewer can
// see it; "" when not.
func (m *model) project(ctx context.Context, workspaceID, key, viewerID string) (id, current string, err error) {
	err = m.db.QueryRow(ctx, `select id::text, key from projects where workspace_id = $1 and key = canonical_key($1, $2)
		and can_see_project(id, $3)`, workspaceID, strings.ToUpper(key), viewerID).Scan(&id, &current)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("find project: %w", err)
	}
	return id, current, nil
}

// issue says whether the id is a live issue in the project, and the project isn't archived.
func (m *model) issue(ctx context.Context, projectID, id string) (bool, error) {
	var ok bool
	err := m.db.QueryRow(ctx, `select exists (select 1 from issues i join projects p on p.id = i.project_id
		where i.id = $1 and i.project_id = $2 and p.archived_at is null)`, id, projectID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("find issue: %w", err)
	}
	return ok, nil
}

// find returns the file when userID may read it (a member of its workspace who, for a project's file, can see the
// project; or anyone for an avatar), else nil.
func (m *model) find(ctx context.Context, id, userID string) (*File, error) {
	f := File{ID: id}
	err := m.db.QueryRow(ctx,
		`select f.name, f.content_type, f.size from files f
		 where f.id = $1 and (f.workspace_id is null or exists (
		   select 1 from workspace_members m where m.workspace_id = f.workspace_id and m.user_id = $2))
		   and (f.project_id is null or can_see_project(f.project_id, $2))`, id, userID).
		Scan(&f.Name, &f.ContentType, &f.Size)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find file: %w", err)
	}
	return &f, nil
}

// avatar returns the user's avatar file id; "" when they have none or don't exist.
func (m *model) avatar(ctx context.Context, userID string) (string, error) {
	var id *string
	err := m.db.QueryRow(ctx, `select avatar_file_id::text from users where id = $1`, userID).Scan(&id)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("read avatar: %w", err)
	}
	if id == nil {
		return "", nil
	}
	return *id, nil
}

// setAvatar points the user at fileID (nil removes it) and deletes the previous avatar's row, returning its id
// ("" when there was none) so the caller can remove the bytes.
func (m *model) setAvatar(ctx context.Context, userID string, fileID *string) (old string, err error) {
	err = pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var prev *string
		err := tx.QueryRow(ctx, `select avatar_file_id::text from users where id = $1 for update`, userID).Scan(&prev)
		if err != nil {
			return fmt.Errorf("read avatar: %w", err)
		}
		if _, err := tx.Exec(ctx, `update users set avatar_file_id = $2 where id = $1`, userID, fileID); err != nil {
			return fmt.Errorf("set avatar: %w", err)
		}
		if prev != nil {
			if _, err := tx.Exec(ctx, `delete from files where id = $1`, *prev); err != nil {
				return fmt.Errorf("delete old avatar: %w", err)
			}
			old = *prev
		}
		return realtime.PublishProfile(ctx, tx, userID)
	})
	return old, err
}
