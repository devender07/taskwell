package file

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/user/internal/storage"
	"github.com/devender07/tether/backend/user/internal/sysconfig"
	"github.com/devender07/tether/backend/user/internal/web"
	"github.com/devender07/tether/backend/user/internal/workspace"
)

const (
	maxAvatar       = 2 << 20
	errChooseFile   = "Choose a file."
	errChooseImage  = "Choose a PNG, JPEG, GIF or WebP image."
	errFileNotFound = "File not found."
	avatarCache     = "private, max-age=300"
)

// Handler serves uploads. Each file is stored as the object <id>: the key is always the server's uuid, never anything
// the client sent.
type Handler struct {
	model        *model
	files        *storage.Store
	authenticate func(*http.Request) (string, error)
	member       func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)
	cfg          *sysconfig.Store
	profile      func(ctx context.Context, userID string) (any, error)
}

// New takes the auth handler's Authenticate and Profile (the GET /api/me user) and the workspace handler's Member.
func New(db *pgxpool.Pool, files *storage.Store, authenticate func(*http.Request) (string, error),
	member func(ctx context.Context, userID, slug string) (*workspace.Workspace, error), cfg *sysconfig.Store,
	profile func(ctx context.Context, userID string) (any, error)) *Handler {
	return &Handler{model: &model{db: db}, files: files, authenticate: authenticate, member: member, cfg: cfg,
		profile: profile}
}

func (h *Handler) upload(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, err := h.member(r.Context(), userID, r.PathValue("slug"))
	if err != nil {
		return err
	}
	if ws == nil {
		web.Err(w, http.StatusNotFound, "Workspace not found.")
		return nil
	}
	mb, err := h.cfg.GetInt(r.Context(), "upload_max_mb")
	if err != nil {
		return err
	}
	quota, err := h.cfg.GetInt(r.Context(), "upload_quota_mb")
	if err != nil {
		return err
	}
	used, err := h.model.used(r.Context(), ws.ID)
	if err != nil {
		return err
	}
	// ponytail: concurrent uploads each see the same room, so the quota can be overshot by a few files.
	limit, tooLarge := int64(mb)<<20, fmt.Sprintf("Files must be %d MB or smaller.", mb)
	if left := int64(quota)<<20 - used; left < limit {
		limit, tooLarge = left, fmt.Sprintf("This workspace is out of storage (%d MB). Delete some attachments or ask "+
			"your administrator for more space.", quota)
	}
	if limit <= 0 {
		web.Err(w, http.StatusRequestEntityTooLarge, tooLarge)
		return nil
	}
	f, ok, err := h.save(w, r, &ws.ID, userID, limit, tooLarge, false)
	if !ok || err != nil {
		return err
	}
	web.JSON(w, http.StatusCreated, f)
	return nil
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request, userID string) error {
	id := r.PathValue("id")
	var f *File
	if web.IsUUID(id) {
		var err error
		if f, err = h.model.find(r.Context(), id, userID); err != nil {
			return err
		}
	}
	if f == nil {
		web.Err(w, http.StatusNotFound, errFileNotFound)
		return nil
	}
	u, err := h.files.URL(r.Context(), f.ID)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", fmt.Sprintf("private, max-age=%d", int(storage.CacheFor.Seconds())))
	http.Redirect(w, r, u, http.StatusFound)
	return nil
}

func (h *Handler) setAvatar(w http.ResponseWriter, r *http.Request, userID string) error {
	f, ok, err := h.save(w, r, nil, userID, maxAvatar, "Images must be 2 MB or smaller.", true)
	if !ok || err != nil {
		return err
	}
	return h.replaceAvatar(w, r, userID, &f.ID)
}

func (h *Handler) removeAvatar(w http.ResponseWriter, r *http.Request, userID string) error {
	return h.replaceAvatar(w, r, userID, nil)
}

// replaceAvatar sets (or with nil, clears) the user's avatar, removes the previous one and answers with the user.
func (h *Handler) replaceAvatar(w http.ResponseWriter, r *http.Request, userID string, fileID *string) error {
	old, err := h.model.setAvatar(r.Context(), userID, fileID)
	if err != nil {
		return err
	}
	if old != "" {
		// The sweep removes the object later if this fails: it has no row any more.
		if err := h.files.Delete(r.Context(), old); err != nil {
			slog.Warn("remove old avatar", "id", old, "err", err)
		}
	}
	u, err := h.profile(r.Context(), userID)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, u)
	return nil
}

func (h *Handler) avatar(w http.ResponseWriter, r *http.Request, _ string) error {
	w.Header().Set("Cache-Control", avatarCache)
	id := r.PathValue("id")
	fileID := ""
	if web.IsUUID(id) {
		var err error
		if fileID, err = h.model.avatar(r.Context(), id); err != nil {
			return err
		}
	}
	if fileID == "" {
		web.Err(w, http.StatusNotFound, errFileNotFound)
		return nil
	}
	http.Redirect(w, r, fileURL(fileID, "avatar"), http.StatusFound)
	return nil
}

// save streams the multipart "file" field to a temp file, then stores and records it, writing a 400 or 413 and returning ok=false when
// the upload is missing, too large, or (with imageOnly) not an image. The bytes never sit in memory as a whole.
func (h *Handler) save(w http.ResponseWriter, r *http.Request, workspaceID *string, userID string, limit int64,
	tooLarge string, imageOnly bool) (*File, bool, error) {
	r.Body = http.MaxBytesReader(w, r.Body, limit+formOverhead)
	fail := func(status int, msg string) (*File, bool, error) {
		web.Fields(w, status, map[string]string{"file": msg})
		return nil, false, nil
	}
	mr, err := r.MultipartReader()
	if err != nil {
		return fail(http.StatusBadRequest, errChooseFile)
	}
	for {
		part, err := mr.NextPart()
		if tooBig(err) {
			return fail(http.StatusRequestEntityTooLarge, tooLarge)
		}
		if err != nil {
			return fail(http.StatusBadRequest, errChooseFile)
		}
		if part.FormName() != "file" || part.FileName() == "" {
			continue
		}
		f := &File{Name: cleanName(part.FileName()), workspaceID: workspaceID, uploaderID: userID}
		out, err := os.CreateTemp("", "tether-upload-")
		if err != nil {
			return nil, false, fmt.Errorf("create temp file: %w", err)
		}
		defer os.Remove(out.Name()) //nolint:errcheck // best effort
		defer out.Close()
		head := make([]byte, 512)
		n, err := io.ReadFull(part, head)
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
			if tooBig(err) {
				return fail(http.StatusRequestEntityTooLarge, tooLarge)
			}
			return fail(http.StatusBadRequest, errChooseFile)
		}
		f.ContentType = http.DetectContentType(head[:n])
		if _, err := out.Write(head[:n]); err != nil {
			return nil, false, fmt.Errorf("write upload: %w", err)
		}
		rest, err := io.Copy(out, io.LimitReader(part, limit-int64(n)+1))
		f.Size = int64(n) + rest
		switch {
		case tooBig(err) || f.Size > limit:
			return fail(http.StatusRequestEntityTooLarge, tooLarge)
		case err != nil:
			return fail(http.StatusBadRequest, errChooseFile)
		case f.Size == 0:
			return fail(http.StatusBadRequest, errChooseFile)
		case imageOnly && !isImage(f.ContentType):
			return fail(http.StatusBadRequest, errChooseImage)
		}
		f.Image = isImage(f.ContentType)
		if _, err := out.Seek(0, io.SeekStart); err != nil {
			return nil, false, fmt.Errorf("rewind upload: %w", err)
		}
		err = h.model.create(r.Context(), f, func(id string) error {
			return h.files.Put(r.Context(), id, f.ContentType, disposition(f.ContentType, f.Name), out)
		})
		if err != nil {
			return nil, false, err
		}
		f.URL = fileURL(f.ID, f.Name)
		return f, true, nil
	}
}

func tooBig(err error) bool {
	var mbe *http.MaxBytesError
	return errors.As(err, &mbe)
}

// --- responses ---
