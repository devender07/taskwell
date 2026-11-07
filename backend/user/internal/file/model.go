package file

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

// liveUpload is an uploads row still in its hour (uploadTTL; the sweep deletes older ones).
const liveUpload = `created_at > now() - interval '1 hour'`

// errUploadGone is create's answer when the upload being finished is no longer there (finished meanwhile or expired).
var errUploadGone = errors.New("upload gone")

// create inserts the file's row and calls store with the new id inside the same transaction, so a row only
// commits once its bytes are stored. A file with an ID is a direct upload being finished: its uploads row goes in the
// same transaction (errUploadGone when it already went) and store is nil, the bytes are already there.
func (m *model) create(ctx context.Context, f *File, store func(id string) error) error {
	pl := f.place
	if pl == nil {
		pl = &Placement{}
	}
	return pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if f.ID != "" {
			tag, err := tx.Exec(ctx, `delete from uploads where id = $1 and user_id = $2 and workspace_id is not distinct from $3
				and `+liveUpload, f.ID, f.uploaderID, f.workspaceID)
			if err != nil {
				return fmt.Errorf("finish upload: %w", err)
			}
			if tag.RowsAffected() == 0 {
				return errUploadGone
			}
		}
		err := tx.QueryRow(ctx,
			`insert into files (id, workspace_id, uploader_id, name, content_type, size, project_id, folder_id, listed, issue_id)
			 values (coalesce(nullif($10, '')::uuid, uuidv7()), $1, $2, $3, $4, $5, nullif($6, '')::uuid, $7, $8, $9) returning id::text`,
			f.workspaceID, f.uploaderID, f.Name, f.ContentType, f.Size, pl.ProjectID, pl.FolderID, pl.Listed, pl.IssueID, f.ID).Scan(&f.ID)
		if err != nil {
			return fmt.Errorf("insert file: %w", err)
		}
		if pl.ProjectID != "" {
			if err := realtime.Publish(ctx, tx, pl.Event); err != nil {
				return err
			}
		}
		if store == nil {
			return nil
		}
		return store(f.ID)
	})
}

// startUpload records a direct upload the client is about to PUT and returns its id, the object key.
func (m *model) startUpload(ctx context.Context, workspaceID *string, userID, name, contentType string, size int64, max int) (string, error) {
	var id string
	_, err := m.locked(ctx, "uploads:"+userID, false, func(tx pgx.Tx) error {
		if max > 0 {
			var n int
			if err := tx.QueryRow(ctx, `select count(*) from uploads where user_id = $1
				and workspace_id is not distinct from $2`, userID, workspaceID).Scan(&n); err != nil {
				return err
			}
			if n >= max {
				return nil
			}
		}
		return tx.QueryRow(ctx, `insert into uploads (workspace_id, user_id, name, content_type, size) values ($1, $2, $3, $4, $5)
			returning id::text`, workspaceID, userID, name, contentType, size).Scan(&id)
	})
	if err != nil {
		return "", fmt.Errorf("start upload: %w", err)
	}
	return id, nil
}

// locked runs fn in a transaction holding an advisory lock on key, so calls with the same key on any instance run one
// at a time. fn queries through tx only: a locked section holds one connection, never waits on the pool for a second.
// With try, it doesn't wait for the lock either: it reports false without running fn when another call holds it.
func (m *model) locked(ctx context.Context, key string, try bool, fn func(tx pgx.Tx) error) (bool, error) {
	got := true
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		lock := `select true from (select pg_advisory_xact_lock(hashtextextended($1, 0))) l`
		if try {
			lock = `select pg_try_advisory_xact_lock(hashtextextended($1, 0))`
		}
		if err := tx.QueryRow(ctx, lock, key).Scan(&got); err != nil {
			return fmt.Errorf("lock %s: %w", key, err)
		}
		if !got {
			return nil
		}
		return fn(tx)
	})
	return got, err
}

// querier is a pool or a transaction.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// upload returns the user's live upload id to the workspace (nil: an avatar) as a File to create, nil when there's none.
func (m *model) upload(ctx context.Context, q querier, id, userID string, workspaceID *string) (*File, error) {
	f := File{ID: id, workspaceID: workspaceID, uploaderID: userID}
	err := q.QueryRow(ctx, `select name, content_type, size from uploads where id = $1 and user_id = $2
		and workspace_id is not distinct from $3 and `+liveUpload, id, userID, workspaceID).Scan(&f.Name, &f.ContentType, &f.Size)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find upload: %w", err)
	}
	return &f, nil
}

// reapUploads deletes the expired uploads to the workspace (nil: the user's avatar uploads) and returns their ids, so
// their staged bytes can go too.
func (m *model) reapUploads(ctx context.Context, workspaceID *string, userID string) ([]string, error) {
	rows, err := m.db.Query(ctx, `delete from uploads where not (`+liveUpload+`)
		and case when $1::uuid is null then workspace_id is null and user_id = $2 else workspace_id = $1 end
		returning id::text`, workspaceID, userID)
	if err != nil {
		return nil, fmt.Errorf("reap uploads: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("reap uploads: %w", err)
	}
	return ids, nil
}

// dropUpload deletes the upload's row, as when its bytes were refused.
func (m *model) dropUpload(ctx context.Context, q querier, id string) error {
	if _, err := q.Exec(ctx, `delete from uploads where id = $1`, id); err != nil {
		return fmt.Errorf("drop upload: %w", err)
	}
	return nil
}

// used returns the bytes the workspace's files and unfinished uploads take up. An expired upload counts until it's
// reaped (reapUploads, or the sweep), since its bytes may still be in the bucket.
func (m *model) used(ctx context.Context, workspaceID string) (int64, error) {
	var n int64
	if err := m.db.QueryRow(ctx, `select (select coalesce(sum(size), 0) from files where workspace_id = $1)
		+ (select coalesce(sum(size), 0) from uploads where workspace_id = $1)`, workspaceID).Scan(&n); err != nil {
		return 0, fmt.Errorf("sum file sizes: %w", err)
	}
	return n, nil
}

// project returns the id and current key of the workspace's project with the key (old keys too) when the viewer can
// see it and it isn't archived; "" when not.
func (m *model) project(ctx context.Context, workspaceID, key, viewerID string) (id, current string, err error) {
	err = m.db.QueryRow(ctx, `select id::text, key from projects where workspace_id = $1 and key = canonical_key($1, $2)
		and can_see_project(id, $3) and archived_at is null`, workspaceID, strings.ToUpper(key), viewerID).Scan(&id, &current)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("find project: %w", err)
	}
	return id, current, nil
}

// issue says whether the id is a live issue in the project.
func (m *model) issue(ctx context.Context, projectID, id string) (bool, error) {
	var ok bool
	err := m.db.QueryRow(ctx, `select exists (select 1 from issues where id = $1 and project_id = $2)`, id, projectID).Scan(&ok)
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
