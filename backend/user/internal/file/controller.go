package file

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/realtime"
	"github.com/devender07/taskwell/backend/user/internal/storage"
	"github.com/devender07/taskwell/backend/user/internal/sysconfig"
	"github.com/devender07/taskwell/backend/user/internal/web"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

const (
	maxAvatar       = 2 << 20
	maxField        = 1 << 10 // a form field beside the file
	errChooseFile   = "Choose a file."
	errChooseImage  = "Choose a PNG, JPEG, GIF or WebP image."
	errChooseMedia  = "Only images (PNG, JPEG, GIF, WebP) and videos (MP4, WebM) can be uploaded."
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
	f, ok, err := h.Upload(w, r, ws.ID, userID, func(fields url.Values) (*Placement, bool, error) {
		key := strings.TrimSpace(fields.Get("project"))
		if key == "" {
			return nil, true, nil
		}
		id, key, err := h.model.project(r.Context(), ws.ID, key, userID)
		if err != nil || id == "" {
			if err == nil {
				web.Err(w, http.StatusNotFound, "Project not found.")
			}
			return nil, false, err
		}
		pl := &Placement{ProjectID: id, Event: realtime.FilesChanged(ws.Slug, ws.ID, id, key, userID)}
		if issueID := strings.TrimSpace(fields.Get("issue")); issueID != "" {
			// Attaching to an issue is adding to it, as a comment is.
			if !workspace.Has(ws.Permissions, workspace.PermCommentsCreate) {
				web.Err(w, http.StatusForbidden, "You can't add files to this issue.")
				return nil, false, nil
			}
			found := false
			if web.IsUUID(issueID) {
				if found, err = h.model.issue(r.Context(), id, issueID); err != nil {
					return nil, false, err
				}
			}
			if !found {
				web.Err(w, http.StatusNotFound, "Issue not found.")
				return nil, false, nil
			}
			pl.IssueID = &issueID
		}
		return pl, true, nil
	})
	if !ok || err != nil {
		return err
	}
	web.JSON(w, http.StatusCreated, f)
	return nil
}

// Upload stores the request's multipart "file" as an upload to the workspace, within upload_max_mb and what's left of
// upload_quota_mb. place, when not nil, gets the form's other fields and says where the file goes (nil: nowhere in
// particular); it writes its own error response and returns ok=false to stop. On ok=false a response was written.
func (h *Handler) Upload(w http.ResponseWriter, r *http.Request, workspaceID, userID string,
	place func(fields url.Values) (*Placement, bool, error)) (*File, bool, error) {
	mb, err := h.cfg.GetInt(r.Context(), "upload_max_mb")
	if err != nil {
		return nil, false, err
	}
	quota, err := h.cfg.GetInt(r.Context(), "upload_quota_mb")
	if err != nil {
		return nil, false, err
	}
	used, err := h.model.used(r.Context(), workspaceID)
	if err != nil {
		return nil, false, err
	}
	// ponytail: concurrent uploads each see the same room, so the quota can be overshot by a few files.
	limit, tooLarge := int64(mb)<<20, fmt.Sprintf("Files must be %d MB or smaller.", mb)
	if left := int64(quota)<<20 - used; left < limit {
		limit, tooLarge = left, fmt.Sprintf("This workspace is out of storage (%d MB). Delete some attachments or ask "+
			"your administrator for more space.", quota)
	}
	if limit <= 0 {
		web.Err(w, http.StatusRequestEntityTooLarge, tooLarge)
		return nil, false, nil
	}
	return h.save(w, r, &workspaceID, userID, limit, tooLarge, false, place)
}

// storage answers the workspace's storage use against its quota.
func (h *Handler) storage(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, err := h.member(r.Context(), userID, r.PathValue("slug"))
	if err != nil {
		return err
	}
	if ws == nil {
		web.Err(w, http.StatusNotFound, "Workspace not found.")
		return nil
	}
	quota, err := h.cfg.GetInt(r.Context(), "upload_quota_mb")
	if err != nil {
		return err
	}
	used, err := h.model.used(r.Context(), ws.ID)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]int64{"used_bytes": used, "quota_bytes": int64(quota) << 20})
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
	f, ok, err := h.save(w, r, nil, userID, maxAvatar, "Images must be 2 MB or smaller.", true, nil)
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
	http.Redirect(w, r, URL(fileID, "avatar"), http.StatusFound)
	return nil
}

// save streams the multipart "file" field to a temp file, then stores and records it, writing a 400 or 413 and returning ok=false when
// the upload is missing, too large, or (with imageOnly) not an image. The bytes never sit in memory as a whole. The
// form's other fields, in any order, go to place (see Upload).
func (h *Handler) save(w http.ResponseWriter, r *http.Request, workspaceID *string, userID string, limit int64,
	tooLarge string, imageOnly bool, place func(url.Values) (*Placement, bool, error)) (*File, bool, error) {
	r.Body = http.MaxBytesReader(w, r.Body, limit+formOverhead)
	fail := func(status int, msg string) (*File, bool, error) {
		web.Fields(w, status, map[string]string{"file": msg})
		return nil, false, nil
	}
	mr, err := r.MultipartReader()
	if err != nil {
		return fail(http.StatusBadRequest, errChooseFile)
	}
	var f *File
	var out *os.File
	fields := url.Values{}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if tooBig(err) {
			return fail(http.StatusRequestEntityTooLarge, tooLarge)
		}
		if err != nil {
			return fail(http.StatusBadRequest, errChooseFile)
		}
		if part.FileName() == "" {
			v, err := io.ReadAll(io.LimitReader(part, maxField))
			if err != nil {
				return fail(http.StatusBadRequest, errChooseFile)
			}
			fields.Add(part.FormName(), string(v))
			continue
		}
		if part.FormName() != "file" || f != nil {
			continue
		}
		f = &File{Name: CleanName(part.FileName()), workspaceID: workspaceID, uploaderID: userID}
		if out, err = os.CreateTemp("", "taskwell-upload-"); err != nil {
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
		case !isMedia(f.ContentType):
			return fail(http.StatusBadRequest, errChooseMedia)
		}
	}
	if f == nil {
		return fail(http.StatusBadRequest, errChooseFile)
	}
	if place != nil {
		pl, ok, err := place(fields)
		if !ok || err != nil {
			return nil, false, err
		}
		f.place = pl
	}
	f.Image = isImage(f.ContentType)
	if _, err := out.Seek(0, io.SeekStart); err != nil {
		return nil, false, fmt.Errorf("rewind upload: %w", err)
	}
	err = h.model.create(r.Context(), f, func(id string) error {
		return h.files.Put(r.Context(), id, f.ContentType, Disposition(f.ContentType, f.Name), out)
	})
	if err != nil {
		return nil, false, err
	}
	f.URL = URL(f.ID, f.Name)
	return f, true, nil
}

func tooBig(err error) bool {
	var mbe *http.MaxBytesError
	return errors.As(err, &mbe)
}

// --- responses ---
