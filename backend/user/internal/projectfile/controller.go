package projectfile

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/file"
	"github.com/devender07/taskwell/backend/user/internal/realtime"
	"github.com/devender07/taskwell/backend/user/internal/storage"
	"github.com/devender07/taskwell/backend/user/internal/web"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

const (
	errProjectNotFound = "Project not found."
	errFolderNotFound  = "Folder not found."
	errFileNotFound    = "File not found."
	errForbidden       = "You don't have permission to do that."
	errNotYours        = "You can only change your own files."
	errNotEmptyMsg     = "Move or delete what's inside first."
)

// Upload is the file handler's Upload.
type Upload func(w http.ResponseWriter, r *http.Request, workspaceID, userID string,
	place func(url.Values) (*file.Placement, bool, error)) (*file.File, bool, error)

type Handler struct {
	model        *model
	files        *storage.Store
	upload       Upload
	authenticate func(*http.Request) (string, error)
	member       func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)
}

// New takes the auth handler's Authenticate, the workspace handler's Member and the file handler's Upload.
func New(db *pgxpool.Pool, files *storage.Store, upload Upload, authenticate func(*http.Request) (string, error),
	member func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)) *Handler {
	return &Handler{model: &model{db: db}, files: files, upload: upload, authenticate: authenticate, member: member}
}

// req is a request in a project the caller can see.
type req struct {
	ws *workspace.Workspace
	p  *project
	v  viewer
}

func (q req) event() realtime.Event {
	return realtime.FilesChanged(q.ws.Slug, q.ws.ID, q.p.id, q.p.key, q.v.id)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request, userID string) error {
	q, ok, err := h.project(w, r, userID)
	if !ok || err != nil {
		return err
	}
	out := struct {
		Folder  *Folder  `json:"folder"`
		Path    []Crumb  `json:"path"`
		Folders []Folder `json:"folders"`
		Files   []File   `json:"files"`
	}{Path: []Crumb{}}
	search, folder := strings.TrimSpace(r.URL.Query().Get("q")), r.URL.Query().Get("folder")
	switch {
	case search != "":
		like := likePattern(search)
		out.Folders, out.Files, err = h.model.list(r.Context(), q.p.id, q.v, nameLike, fileLike, like)
	case r.URL.Query().Has("issue"):
		// An issue's files (by its id); a trashed issue or one in another project lists none.
		issue := r.URL.Query().Get("issue")
		if !web.IsUUID(issue) {
			web.Err(w, http.StatusNotFound, "Issue not found.")
			return nil
		}
		out.Folders, out.Files, err = h.model.list(r.Context(), q.p.id, q.v, noFolderFor, issueFiles, issue)
	case folder == "attachments":
		out.Folders, out.Files, err = h.model.list(r.Context(), q.p.id, q.v, noFolders, attachments, nil)
	case folder == "":
		out.Folders, out.Files, err = h.model.list(r.Context(), q.p.id, q.v, inFolder, filesIn, nil)
	default:
		if web.IsUUID(folder) {
			if out.Folder, err = h.model.folder(r.Context(), q.p.id, folder, q.v); err != nil {
				return err
			}
		}
		if out.Folder == nil {
			web.Err(w, http.StatusNotFound, errFolderNotFound)
			return nil
		}
		if out.Path, err = h.model.path(r.Context(), folder); err != nil {
			return err
		}
		out.Folders, out.Files, err = h.model.list(r.Context(), q.p.id, q.v, inFolder, filesIn, folder)
	}
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, out)
	return nil
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request, userID string) error {
	q, ok, err := h.contribute(w, r, userID)
	if !ok || err != nil {
		return err
	}
	f, ok, err := h.upload(w, r, q.ws.ID, userID, func(fields url.Values) (*file.Placement, bool, error) {
		folderID, ok, err := h.folderField(w, r, q, fields, "folder_id")
		if !ok || err != nil {
			return nil, false, err
		}
		return &file.Placement{ProjectID: q.p.id, FolderID: folderID, Listed: true, Event: q.event()}, true, nil
	})
	if !ok || err != nil {
		return err
	}
	got, err := h.model.file(r.Context(), q.p.id, f.ID, q.v)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusCreated, got)
	return nil
}

func (h *Handler) updateFile(w http.ResponseWriter, r *http.Request, userID string) error {
	q, f, ok, err := h.editFile(w, r, userID, false)
	if !ok || err != nil {
		return err
	}
	form := readForm(r)
	name, ok := nameField(w, form, maxFileName)
	if !ok {
		return nil
	}
	if name != nil {
		*name = file.CleanName(*name)
	}
	_, move := form["folder_id"]
	folderID, ok, err := h.folderField(w, r, q, form, "folder_id")
	if !ok || err != nil {
		return err
	}
	got, err := h.model.updateFile(r.Context(), q.p.id, f.ID, name, move, folderID, q.v, q.event())
	if err != nil {
		return answer(w, err)
	}
	if name != nil && *name != f.Name {
		// A download is named by the stored object's headers; the sweep never touches them, so a failure here
		// only leaves the old name on downloads.
		if err := h.files.SetHeaders(r.Context(), f.ID, got.ContentType, file.Disposition(got.ContentType, got.Name)); err != nil {
			slog.Warn("rename stored file", "id", f.ID, "err", err)
		}
	}
	web.JSON(w, http.StatusOK, got)
	return nil
}

func (h *Handler) deleteFile(w http.ResponseWriter, r *http.Request, userID string) error {
	q, f, ok, err := h.editFile(w, r, userID, true)
	if !ok || err != nil {
		return err
	}
	deleted, err := h.model.deleteFile(r.Context(), q.p.id, f.ID, q.event())
	if err != nil {
		return err
	}
	if !deleted {
		web.Err(w, http.StatusNotFound, errFileNotFound)
		return nil
	}
	// The sweep removes the object later if this fails: it has no row any more.
	if err := h.files.Delete(r.Context(), f.ID); err != nil {
		slog.Warn("remove deleted file", "id", f.ID, "err", err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) createFolder(w http.ResponseWriter, r *http.Request, userID string) error {
	q, ok, err := h.contribute(w, r, userID)
	if !ok || err != nil {
		return err
	}
	form := readForm(r)
	if _, sent := form["name"]; !sent {
		form.Set("name", "") // required on create
	}
	name, ok := nameField(w, form, maxFolderName)
	if !ok {
		return nil
	}
	parentID, ok, err := h.folderField(w, r, q, form, "parent_id")
	if !ok || err != nil {
		return err
	}
	d, err := h.model.createFolder(r.Context(), q.p.id, parentID, *name, q.v, q.event())
	if err != nil {
		return answer(w, err)
	}
	web.JSON(w, http.StatusCreated, d)
	return nil
}

func (h *Handler) updateFolder(w http.ResponseWriter, r *http.Request, userID string) error {
	q, d, ok, err := h.editFolder(w, r, userID)
	if !ok || err != nil {
		return err
	}
	form := readForm(r)
	name, ok := nameField(w, form, maxFolderName)
	if !ok {
		return nil
	}
	_, move := form["parent_id"]
	parentID, ok, err := h.folderField(w, r, q, form, "parent_id")
	if !ok || err != nil {
		return err
	}
	got, err := h.model.updateFolder(r.Context(), q.p.id, d.ID, name, move, parentID, q.v, q.event())
	if err != nil {
		return answer(w, err)
	}
	web.JSON(w, http.StatusOK, got)
	return nil
}

func (h *Handler) deleteFolder(w http.ResponseWriter, r *http.Request, userID string) error {
	q, d, ok, err := h.editFolder(w, r, userID)
	if !ok || err != nil {
		return err
	}
	if err := h.model.deleteFolder(r.Context(), q.p.id, d.ID, q.event()); err != nil {
		return answer(w, err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// answer writes the response for a model error the API names and returns nil; any other err is returned for wrap.
func answer(w http.ResponseWriter, err error) error {
	switch {
	case errors.Is(err, errNoFolder):
		web.Err(w, http.StatusNotFound, errFolderNotFound)
	case errors.Is(err, errNameTaken):
		web.Fields(w, http.StatusBadRequest, map[string]string{"name": "A folder with this name already exists."})
	case errors.Is(err, errInside):
		web.Fields(w, http.StatusBadRequest, map[string]string{"parent_id": "A folder can't go inside itself."})
	case errors.Is(err, errNotEmpty):
		web.Err(w, http.StatusConflict, errNotEmptyMsg)
	default:
		return err
	}
	return nil
}

// project resolves the path's workspace and project, or writes a 404 (for a non-member too) and returns ok=false.
func (h *Handler) project(w http.ResponseWriter, r *http.Request, userID string) (req, bool, error) {
	ws, err := h.member(r.Context(), userID, r.PathValue("slug"))
	if err != nil {
		return req{}, false, err
	}
	var p *project
	if ws != nil {
		if p, err = h.model.project(r.Context(), ws.ID, strings.ToUpper(strings.TrimSpace(r.PathValue("key"))), userID); err != nil {
			return req{}, false, err
		}
	}
	if p == nil {
		web.Err(w, http.StatusNotFound, errProjectNotFound)
		return req{}, false, nil
	}
	return req{ws: ws, p: p, v: viewer{id: userID, manage: workspace.Has(ws.Permissions, workspace.PermProjectsManage)}}, true, nil
}

// contribute is project for adding files and folders, which needs comments.create; it writes a 403 without.
func (h *Handler) contribute(w http.ResponseWriter, r *http.Request, userID string) (req, bool, error) {
	q, ok, err := h.project(w, r, userID)
	if ok && !workspace.Has(q.ws.Permissions, workspace.PermCommentsCreate) {
		web.Err(w, http.StatusForbidden, errForbidden)
		return req{}, false, nil
	}
	return q, ok, err
}

// editFile is project plus the path's file, which the caller must own or manage; else a 404 or 403. Attachments
// (not listed) are found only when attachments is set.
func (h *Handler) editFile(w http.ResponseWriter, r *http.Request, userID string, attachments bool) (req, *File, bool, error) {
	q, ok, err := h.project(w, r, userID)
	if !ok || err != nil {
		return req{}, nil, false, err
	}
	var f *File
	if id := r.PathValue("id"); web.IsUUID(id) {
		if f, err = h.model.file(r.Context(), q.p.id, id, q.v); err != nil {
			return req{}, nil, false, err
		}
	}
	if f == nil || (!f.listed && !attachments) {
		web.Err(w, http.StatusNotFound, errFileNotFound)
		return req{}, nil, false, nil
	}
	if !f.CanEdit {
		web.Err(w, http.StatusForbidden, errNotYours)
		return req{}, nil, false, nil
	}
	return q, f, true, nil
}

// editFolder is editFile for the path's folder.
func (h *Handler) editFolder(w http.ResponseWriter, r *http.Request, userID string) (req, *Folder, bool, error) {
	q, ok, err := h.project(w, r, userID)
	if !ok || err != nil {
		return req{}, nil, false, err
	}
	var d *Folder
	if id := r.PathValue("id"); web.IsUUID(id) {
		if d, err = h.model.folder(r.Context(), q.p.id, id, q.v); err != nil {
			return req{}, nil, false, err
		}
	}
	if d == nil {
		web.Err(w, http.StatusNotFound, errFolderNotFound)
		return req{}, nil, false, nil
	}
	if !d.CanEdit {
		web.Err(w, http.StatusForbidden, errNotYours)
		return req{}, nil, false, nil
	}
	return q, d, true, nil
}

// folderField is the form's folder id under key ("" or missing: the root, nil), or writes a 404 when it isn't one of
// the project's folders and returns ok=false.
func (h *Handler) folderField(w http.ResponseWriter, r *http.Request, q req, form url.Values, key string) (*string, bool, error) {
	id := strings.TrimSpace(form.Get(key))
	if id == "" {
		return nil, true, nil
	}
	found := false
	if web.IsUUID(id) {
		var err error
		if found, err = h.model.hasFolder(r.Context(), q.p.id, id); err != nil {
			return nil, false, err
		}
	}
	if !found {
		web.Err(w, http.StatusNotFound, errFolderNotFound)
		return nil, false, nil
	}
	return &id, true, nil
}

const (
	maxFileName   = 200
	maxFolderName = 100
)

func readForm(r *http.Request) url.Values {
	_ = r.ParseMultipartForm(maxBody)
	return r.PostForm
}

// nameField is the form's trimmed "name" (nil when it's not sent), or writes a 400 when it's empty or longer than max
// characters and returns ok=false.
func nameField(w http.ResponseWriter, form url.Values, max int) (*string, bool) {
	if _, sent := form["name"]; !sent {
		return nil, true
	}
	name := strings.TrimSpace(form.Get("name"))
	msg := ""
	switch {
	case name == "":
		msg = "Enter a name."
	case utf8.RuneCountInString(name) > max:
		msg = "Names can be up to " + strconv.Itoa(max) + " characters."
	}
	if !web.Check(w, map[string]string{"name": msg}) {
		return nil, false
	}
	return &name, true
}
