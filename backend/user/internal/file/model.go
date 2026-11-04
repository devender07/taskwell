package file

import (
	"context"
	"errors"
	"fmt"

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
}

// create inserts the file's row and calls store with the new id inside the same transaction, so a row only
// commits once its bytes are stored.
func (m *model) create(ctx context.Context, f *File, store func(id string) error) error {
	return pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx,
			`insert into files (workspace_id, uploader_id, name, content_type, size) values ($1, $2, $3, $4, $5)
			 returning id::text`,
			f.workspaceID, f.uploaderID, f.Name, f.ContentType, f.Size).Scan(&f.ID)
		if err != nil {
			return fmt.Errorf("insert file: %w", err)
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

// find returns the file when userID may read it (a member of its workspace, or anyone for an avatar), else nil.
func (m *model) find(ctx context.Context, id, userID string) (*File, error) {
	f := File{ID: id}
	err := m.db.QueryRow(ctx,
		`select f.name, f.content_type, f.size from files f
		 where f.id = $1 and (f.workspace_id is null or exists (
		   select 1 from workspace_members m where m.workspace_id = f.workspace_id and m.user_id = $2))`, id, userID).
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
