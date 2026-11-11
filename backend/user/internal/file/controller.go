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
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/plan"
	"github.com/devender07/taskwell/backend/user/internal/realtime"
	"github.com/devender07/taskwell/backend/user/internal/storage"
	"github.com/devender07/taskwell/backend/user/internal/sysconfig"
	"github.com/devender07/taskwell/backend/user/internal/web"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

const (
	maxAvatar        = 2 << 20
	maxAvatarUploads = 5       // unfinished at once per user
	maxField         = 1 << 10 // a form field beside the file
	errChooseFile    = "Choose a file."
	errChooseImage   = "Choose a PNG, JPEG, GIF or WebP image."
	errChooseMedia   = "Only images (PNG, JPEG, GIF, WebP) and videos (MP4, WebM) can be uploaded."
	errFileNotFound  = "File not found."
	errNoUpload      = "Upload not found. Try again."
	errFinishing     = "This upload is already being saved."
	tooLargeAvatar   = "Images must be 2 MB or smaller."
	avatarCache      = "private, max-age=300"
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
// its plan's storage. place, when not nil, gets the form's other fields and says where the file goes (nil: nowhere in
// particular); it writes its own error response and returns ok=false to stop. On ok=false a response was written.
func (h *Handler) Upload(w http.ResponseWriter, r *http.Request, workspaceID, userID string,
	place func(fields url.Values) (*Placement, bool, error)) (*File, bool, error) {
	limit, tooLarge, err := h.room(r.Context(), workspaceID)
	if err != nil {
		return nil, false, err
	}
	return h.save(w, r, &workspaceID, userID, limit, tooLarge, false, place)
}

// room returns how many bytes a new upload to the workspace may have (0 or less: none) and the message when it has
// more. Live direct uploads count as used.
// ponytail: concurrent uploads each see the same room, so the quota can be overshot by a few files; lock the
// workspace row while checking and recording if that matters.
func (h *Handler) room(ctx context.Context, workspaceID string) (int64, string, error) {
	mb, err := h.cfg.GetInt(ctx, "upload_max_mb")
	if err != nil {
		return 0, "", err
	}
	p, err := plan.ForWorkspace(ctx, h.model.db, workspaceID)
	if err != nil {
		return 0, "", err
	}
	limit, tooLarge := int64(mb)<<20, fmt.Sprintf("Files must be %d MB or smaller.", mb)
	if p.StorageBytes == 0 {
		return limit, tooLarge, nil
	}
	used, err := h.model.used(ctx, workspaceID)
	if err != nil {
		return 0, "", err
	}
	if left := p.StorageBytes - used; left < limit {
		limit, tooLarge = left, fmt.Sprintf("This workspace is out of storage (%s). Delete some files or ask its owner to upgrade.",
			plan.GB(p.StorageBytes))
	}
	return limit, tooLarge, nil
}

// startUpload begins a direct upload to the workspace: the client PUTs the bytes to the presigned URL it answers with,
// then finishes it by posting file_id to an upload endpoint.
func (h *Handler) startUpload(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, err := h.member(r.Context(), userID, r.PathValue("slug"))
	if err != nil {
		return err
	}
	if ws == nil {
		web.Err(w, http.StatusNotFound, "Workspace not found.")
		return nil
	}
	if err := h.reap(r.Context(), &ws.ID, userID); err != nil {
		return err
	}
	limit, tooLarge, err := h.room(r.Context(), ws.ID)
	if err != nil {
		return err
	}
	return h.start(w, r, &ws.ID, userID, limit, tooLarge, false)
}

// reap drops the workspace's expired uploads (nil: the user's avatar uploads) and their staged bytes, so they stop
// counting against the quota.
func (h *Handler) reap(ctx context.Context, workspaceID *string, userID string) error {
	ids, err := h.model.reapUploads(ctx, workspaceID, userID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := h.files.Delete(ctx, storage.StagePrefix+id); err != nil {
			slog.Warn("delete expired upload", "id", id, "err", err) // the sweep removes it later
		}
	}
	return nil
}

// startAvatarUpload begins a direct upload of an avatar, finished by POST /api/me/avatar.
// Avatars have no quota, so a user may only have a few unfinished at once.
func (h *Handler) startAvatarUpload(w http.ResponseWriter, r *http.Request, userID string) error {
	if err := h.reap(r.Context(), nil, userID); err != nil {
		return err
	}
	return h.start(w, r, nil, userID, maxAvatar, tooLargeAvatar, true)
}

// start checks the form's name, size and type against limit (and with imageOnly, images only), records the upload and
// answers with where and how to PUT it.
func (h *Handler) start(w http.ResponseWriter, r *http.Request, workspaceID *string, userID string, limit int64,
	tooLarge string, imageOnly bool) error {
	_ = r.ParseMultipartForm(maxStartBody)
	name := strings.TrimSpace(r.PostFormValue("name"))
	size, err := strconv.ParseInt(strings.TrimSpace(r.PostFormValue("size")), 10, 64)
	contentType := strings.TrimSpace(r.PostFormValue("type"))
	switch {
	case name == "" || err != nil || size <= 0:
		web.Fields(w, http.StatusBadRequest, map[string]string{"file": errChooseFile})
		return nil
	case imageOnly && !isImage(contentType):
		web.Fields(w, http.StatusBadRequest, map[string]string{"file": errChooseImage})
		return nil
	case !isMedia(contentType):
		web.Fields(w, http.StatusBadRequest, map[string]string{"file": errChooseMedia})
		return nil
	case size > limit:
		web.Fields(w, http.StatusRequestEntityTooLarge, map[string]string{"file": tooLarge})
		return nil
	}
	name = CleanName(name)
	limitN := 0
	if workspaceID == nil {
		limitN = maxAvatarUploads
	}
	id, err := h.model.startUpload(r.Context(), workspaceID, userID, name, contentType, size, limitN)
	if err != nil {
		return err
	}
	if id == "" {
		web.Err(w, http.StatusTooManyRequests, "Too many uploads at once. Try again in an hour.")
		return nil
	}
	u, headers, err := h.files.PresignPut(r.Context(), id, contentType, Disposition(contentType, name), size)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusCreated, map[string]any{"id": id, "url": u, "headers": headers})
	return nil
}

// finish checks the direct upload id the client says it PUT: its row is the user's to this workspace (nil: an
// avatar) and still live, and the stored object has the declared size, headers and type. It returns the file to
// create, or the message to refuse it with. Refused bytes are deleted.
// Finishes of one upload run one at a time, so a second can't promote newly staged bytes over a copy the first checked.
func (h *Handler) finish(ctx context.Context, id string, workspaceID *string, userID string, imageOnly bool) (*File, string, error) {
	if !web.IsUUID(id) {
		return nil, errNoUpload, nil
	}
	var f *File
	var msg string
	got, err := h.model.locked(ctx, "upload:"+id, true, func(tx pgx.Tx) error {
		var err error
		f, msg, err = h.finishLocked(ctx, tx, id, workspaceID, userID, imageOnly)
		return err
	})
	if err == nil && !got {
		return nil, errFinishing, nil
	}
	return f, msg, err
}

func (h *Handler) finishLocked(ctx context.Context, tx pgx.Tx, id string, workspaceID *string, userID string, imageOnly bool) (*File, string, error) {
	f, err := h.model.upload(ctx, tx, id, userID, workspaceID)
	if err != nil || f == nil {
		return nil, errNoUpload, err
	}
	// Checks run on the promoted copy, which only the API can write: the client's PUT only ever reached the staged key.
	obj, err := h.files.Head(ctx, id)
	if err != nil {
		return nil, "", err
	}
	if obj == nil {
		promoted, err := h.files.Promote(ctx, id)
		if err != nil || !promoted {
			return nil, errNoUpload, err // not PUT yet: the upload stays live for a retry
		}
		if obj, err = h.files.Head(ctx, id); err != nil || obj == nil {
			return nil, errNoUpload, err
		}
	}
	ok := obj.Size == f.Size
	if ok {
		head, err := h.files.Start(ctx, id, 512)
		if err != nil {
			return nil, "", err
		}
		// The store may not hold the client to the signed headers (SeaweedFS doesn't), so check what it stored.
		got := http.DetectContentType(head)
		ok = got == f.ContentType && obj.ContentType == f.ContentType && obj.Disposition == Disposition(f.ContentType, f.Name) &&
			(isImage(got) || !imageOnly && isMedia(got))
	}
	if ok {
		return f, "", nil
	}
	if err := h.files.Delete(ctx, id); err != nil {
		return nil, "", err
	}
	if err := h.model.dropUpload(ctx, tx, id); err != nil {
		return nil, "", err
	}
	if obj.Size != f.Size {
		return nil, errNoUpload, nil
	}
	if imageOnly {
		return nil, errChooseImage, nil
	}
	return nil, errChooseMedia, nil
}

// FinishVideo saves the user's direct upload to the workspace as a file in no project when it's an MP4 or WebM of at
// most limit bytes. Otherwise msg says why not, and bytes that aren't such a video are deleted.
func (h *Handler) FinishVideo(ctx context.Context, workspaceID, userID, uploadID string, limit int64) (f *File, msg string, err error) {
	f, msg, err = h.finish(ctx, uploadID, &workspaceID, userID, false)
	if err != nil || msg != "" {
		return nil, msg, err
	}
	if !slices.Contains(videoTypes, f.ContentType) || f.Size > limit {
		if err := h.files.Delete(ctx, f.ID); err != nil {
			return nil, "", err
		}
		if err := h.model.dropUpload(ctx, h.model.db, f.ID); err != nil {
			return nil, "", err
		}
		return nil, fmt.Sprintf("Upload an MP4 or WebM video of %d MB or less.", limit>>20), nil
	}
	err = h.model.create(ctx, f, nil)
	if errors.Is(err, errUploadGone) {
		return nil, errNoUpload, nil
	}
	if err != nil {
		return nil, "", err
	}
	f.URL = URL(f.ID, f.Name)
	return f, "", nil
}

// storage answers the workspace's storage use against its plan's quota (null: unlimited).
func (h *Handler) storage(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, err := h.member(r.Context(), userID, r.PathValue("slug"))
	if err != nil {
		return err
	}
	if ws == nil {
		web.Err(w, http.StatusNotFound, "Workspace not found.")
		return nil
	}
	p, err := plan.ForWorkspace(r.Context(), h.model.db, ws.ID)
	if err != nil {
		return err
	}
	used, err := h.model.used(r.Context(), ws.ID)
	if err != nil {
		return err
	}
	var quota *int64 // null: unlimited
	if p.StorageBytes > 0 {
		quota = &p.StorageBytes
	}
	web.JSON(w, http.StatusOK, map[string]any{"used_bytes": used, "quota_bytes": quota})
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
	cdn, err := h.cfg.Get(r.Context(), "files_cdn_url")
	if err != nil {
		return err
	}
	secret, err := h.cfg.Get(r.Context(), "files_cdn_secret")
	if err != nil {
		return err
	}
	if cdn != "" && secret != "" {
		// The link is the same all day and works for at least 24 hours, so browsers and the edge can cache it.
		now := time.Now()
		next := now.Truncate(24 * time.Hour).Add(24 * time.Hour) // the next UTC midnight
		exp := next.Add(24 * time.Hour).Unix()
		path := "/" + f.ID + "/" + url.PathEscape(f.Name)
		w.Header().Set("Cache-Control", fmt.Sprintf("private, max-age=%d", int(next.Sub(now).Seconds())))
		http.Redirect(w, r, strings.TrimRight(cdn, "/")+path+"?exp="+strconv.FormatInt(exp, 10)+"&sig="+sign(secret, path, exp),
			http.StatusFound)
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
	f, ok, err := h.save(w, r, nil, userID, maxAvatar, tooLargeAvatar, true, nil)
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
		// No body: this answers an <img>, and a JSON body there is blocked as cross-origin data (ERR_BLOCKED_BY_ORB)
		// instead of just failing to the initials fallback. Cached like a photo, so it isn't asked again per render.
		w.WriteHeader(http.StatusNotFound)
		return nil
	}
	http.Redirect(w, r, URL(fileID, "avatar"), http.StatusFound)
	return nil
}

// save streams the multipart "file" field to a temp file, then stores and records it, writing a 400 or 413 and returning ok=false when
// the upload is missing, too large, or (with imageOnly) not an image. The bytes never sit in memory as a whole. The
// form's other fields, in any order, go to place (see Upload). Without a "file" part, a "file_id" field finishes a
// direct upload instead (see finish); its size was checked against the limit when it started.
func (h *Handler) save(w http.ResponseWriter, r *http.Request, workspaceID *string, userID string, limit int64,
	tooLarge string, imageOnly bool, place func(url.Values) (*Placement, bool, error)) (*File, bool, error) {
	r.Body = http.MaxBytesReader(w, r.Body, max(limit, 0)+formOverhead)
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
		if limit <= 0 {
			web.Err(w, http.StatusRequestEntityTooLarge, tooLarge)
			return nil, false, nil
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
		id := strings.TrimSpace(fields.Get("file_id"))
		if id == "" {
			return fail(http.StatusBadRequest, errChooseFile)
		}
		var msg string
		if f, msg, err = h.finish(r.Context(), id, workspaceID, userID, imageOnly); err != nil || msg != "" {
			if err != nil {
				return nil, false, err
			}
			return fail(http.StatusBadRequest, msg)
		}
	}
	if place != nil {
		pl, ok, err := place(fields)
		if !ok || err != nil {
			return nil, false, err
		}
		f.place = pl
	}
	f.Image = isImage(f.ContentType)
	var store func(id string) error // nil: a direct upload, already stored
	if out != nil {
		if _, err := out.Seek(0, io.SeekStart); err != nil {
			return nil, false, fmt.Errorf("rewind upload: %w", err)
		}
		store = func(id string) error {
			return h.files.Put(r.Context(), id, f.ContentType, Disposition(f.ContentType, f.Name), out)
		}
	}
	err = h.model.create(r.Context(), f, store)
	if errors.Is(err, errUploadGone) {
		return fail(http.StatusBadRequest, errNoUpload)
	}
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
