package server

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ponytail: the API's default upload_max_mb; a workspace with a different limit still gets the API's own 413.
const maxUpload = 10 << 20

const uploadNote = " Only images (PNG, JPEG, GIF, WebP) and videos (MP4, WebM), 10 MB or smaller; archived projects are read-only."

// fileTools registers the tools for project files and issue attachments.
func (t *tools) fileTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "list_project_files", Annotations: readOnly,
		Description: "List a project's Files: the folders and files in a folder (the root by default), or the attachments " +
			"uploaded from its issues and comments (folder \"attachments\"), or every name matching query."}, t.listProjectFiles)
	mcp.AddTool(s, &mcp.Tool{Name: "upload_project_file",
		Description: "Upload a file to a project's Files, at the root or in a folder, from base64 content." + uploadNote}, t.uploadProjectFile)
	mcp.AddTool(s, &mcp.Tool{Name: "update_project_file",
		Description: "Rename a project file or move it to another folder (an empty folder moves it to the root)."}, t.updateProjectFile)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_project_file", Annotations: destructive,
		Description: "Delete a project file or attachment. You can delete your own, or anyone's with projects.manage."}, t.deleteProjectFile)
	mcp.AddTool(s, &mcp.Tool{Name: "create_file_folder",
		Description: "Create a folder in a project's Files, at the root or inside parent."}, t.createFileFolder)
	mcp.AddTool(s, &mcp.Tool{Name: "update_file_folder",
		Description: "Rename a folder or move it inside another (an empty parent moves it to the root)."}, t.updateFileFolder)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_file_folder", Annotations: destructive,
		Description: "Delete an empty folder."}, t.deleteFileFolder)
	mcp.AddTool(s, &mcp.Tool{Name: "list_issue_attachments", Annotations: readOnly,
		Description: "List the files attached to an issue."}, t.listIssueAttachments)
	mcp.AddTool(s, &mcp.Tool{Name: "attach_file",
		Description: "Attach a file to an issue from base64 content." + uploadNote}, t.attachFile)
	mcp.AddTool(s, &mcp.Tool{Name: "remove_attachment", Annotations: destructive,
		Description: "Remove a file attached to an issue."}, t.removeAttachment)
	mcp.AddTool(s, &mcp.Tool{Name: "read_file", Annotations: readOnly,
		Description: "Open a Taskwell file by its url: an image comes back as the image, a video as its url, type and size."}, t.readFile)
}

// --- inputs ---

type projectFilesIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Project   string `json:"project" jsonschema:"project key, such as ENG"`
	Folder    string `json:"folder,omitempty" jsonschema:"folder id from list_project_files, or attachments; omit for the root"`
	Query     string `json:"query,omitempty" jsonschema:"words in file and folder names, searched across the whole project"`
}

type uploadIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Project   string `json:"project" jsonschema:"project key, such as ENG"`
	Filename  string `json:"filename" jsonschema:"file name with its extension, such as mockup.png"`
	Content   string `json:"content_base64" jsonschema:"the file's bytes, base64 encoded"`
	Folder    string `json:"folder,omitempty" jsonschema:"folder id from list_project_files; omit for the root"`
}

type updateFileIn struct {
	Workspace string  `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Project   string  `json:"project" jsonschema:"project key, such as ENG"`
	FileID    string  `json:"file_id" jsonschema:"file id from list_project_files"`
	Name      *string `json:"name,omitempty" jsonschema:"new name"`
	Folder    *string `json:"folder,omitempty" jsonschema:"folder id to move it to; empty for the root"`
}

type fileIDIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Project   string `json:"project" jsonschema:"project key, such as ENG"`
	FileID    string `json:"file_id" jsonschema:"file id from list_project_files"`
}

type createFolderIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Project   string `json:"project" jsonschema:"project key, such as ENG"`
	Name      string `json:"name" jsonschema:"folder name, up to 100 characters"`
	Parent    string `json:"parent,omitempty" jsonschema:"id of the folder it goes in; omit for the root"`
}

type updateFolderIn struct {
	Workspace string  `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Project   string  `json:"project" jsonschema:"project key, such as ENG"`
	FolderID  string  `json:"folder_id" jsonschema:"folder id from list_project_files"`
	Name      *string `json:"name,omitempty" jsonschema:"new name"`
	Parent    *string `json:"parent,omitempty" jsonschema:"id of the folder to move it into; empty for the root"`
}

type folderIDIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Project   string `json:"project" jsonschema:"project key, such as ENG"`
	FolderID  string `json:"folder_id" jsonschema:"folder id from list_project_files"`
}

type attachIn struct {
	Workspace  string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Identifier string `json:"identifier" jsonschema:"issue identifier, such as ENG-142"`
	Filename   string `json:"filename" jsonschema:"file name with its extension, such as screenshot.png"`
	Content    string `json:"content_base64" jsonschema:"the file's bytes, base64 encoded"`
}

type attachmentIn struct {
	Workspace  string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Identifier string `json:"identifier" jsonschema:"issue identifier, such as ENG-142"`
	FileID     string `json:"file_id" jsonschema:"file id from list_issue_attachments"`
}

type readFileIn struct {
	URL string `json:"url" jsonschema:"a file url from list_project_files, list_issue_attachments or an upload"`
}

// --- API shapes ---

type projectFile struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	ContentType string  `json:"content_type"`
	Size        int64   `json:"size"`
	Uploader    *person `json:"uploader"`
	CreatedAt   string  `json:"created_at"`
	URL         string  `json:"url"`
	CanEdit     bool    `json:"can_edit"`
}

type fileFolder struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	ParentID  *string `json:"parent_id"`
	CreatedBy *person `json:"created_by"`
	CreatedAt string  `json:"created_at"`
	CanEdit   bool    `json:"can_edit"`
}

type fileList struct {
	Folder  *fileFolder   `json:"folder"`
	Path    []person      `json:"path"` // {id, name} crumbs
	Folders []fileFolder  `json:"folders"`
	Files   []projectFile `json:"files"`
}

// --- outputs ---

type fileOut struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	Uploader    string `json:"uploader,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
	URL         string `json:"url"`
	CanEdit     bool   `json:"can_edit"`
}

type folderOut struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Parent    string `json:"parent,omitempty"`
	CreatedBy string `json:"created_by,omitempty"`
	CanEdit   bool   `json:"can_edit"`
}

func (t *tools) fileOut(f projectFile) fileOut {
	return fileOut{ID: f.ID, Name: f.Name, ContentType: f.ContentType, Size: f.Size, Uploader: name(f.Uploader),
		CreatedAt: f.CreatedAt, URL: t.app + f.URL, CanEdit: f.CanEdit}
}

func folderOutOf(d fileFolder) folderOut {
	o := folderOut{ID: d.ID, Name: d.Name, CreatedBy: name(d.CreatedBy), CanEdit: d.CanEdit}
	if d.ParentID != nil {
		o.Parent = *d.ParentID
	}
	return o
}

func (t *tools) filesOut(l fileList) map[string]any {
	files := make([]fileOut, 0, len(l.Files))
	for _, f := range l.Files {
		files = append(files, t.fileOut(f))
	}
	folders := make([]folderOut, 0, len(l.Folders))
	for _, d := range l.Folders {
		folders = append(folders, folderOutOf(d))
	}
	out := map[string]any{"folders": folders, "files": files}
	if l.Folder != nil {
		out["folder"] = folderOutOf(*l.Folder)
		out["path"] = l.Path
	}
	return out
}

// decodeUpload is the base64 content's bytes, refused before decoding when it's over maxUpload.
func decodeUpload(filename, content string) ([]byte, error) {
	if strings.TrimSpace(filename) == "" {
		return nil, errors.New("Give the file a name.")
	}
	content = strings.TrimSpace(content)
	if base64.StdEncoding.DecodedLen(len(content)) > maxUpload+2 { // +2: DecodedLen counts padding as bytes
		return nil, errors.New("Files must be 10 MB or smaller.")
	}
	data, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		return nil, errors.New("content_base64 isn't valid base64.")
	}
	if len(data) == 0 {
		return nil, errors.New("The file is empty.")
	}
	if len(data) > maxUpload {
		return nil, errors.New("Files must be 10 MB or smaller.")
	}
	return data, nil
}

// issueRef is the issue's id and its project's key, which the files API takes.
func (t *tools) issueRef(ctx context.Context, slug, identifier string) (id, key string, err error) {
	var i struct {
		ID      string `json:"id"`
		Project struct {
			Key string `json:"key"`
		} `json:"project"`
	}
	if err := t.api.get(ctx, ws(slug, "issues", identifier), nil, &i); err != nil {
		return "", "", err
	}
	return i.ID, i.Project.Key, nil
}

// --- tools ---

func (t *tools) listProjectFiles(ctx context.Context, _ *mcp.CallToolRequest, in projectFilesIn) (*mcp.CallToolResult, any, error) {
	q := url.Values{}
	if in.Query != "" {
		q.Set("q", in.Query)
	}
	if in.Folder != "" {
		q.Set("folder", in.Folder)
	}
	var l fileList
	if err := t.api.get(ctx, ws(in.Workspace, "projects", in.Project, "files"), q, &l); err != nil {
		return nil, nil, err
	}
	return result(t.filesOut(l))
}

func (t *tools) uploadProjectFile(ctx context.Context, _ *mcp.CallToolRequest, in uploadIn) (*mcp.CallToolResult, any, error) {
	data, err := decodeUpload(in.Filename, in.Content)
	if err != nil {
		return nil, nil, err
	}
	var f projectFile
	fields := url.Values{"folder_id": {in.Folder}}
	if err := t.api.upload(ctx, ws(in.Workspace, "projects", in.Project, "files"), fields, in.Filename, data, &f); err != nil {
		return nil, nil, err
	}
	return result(t.fileOut(f))
}

func (t *tools) updateProjectFile(ctx context.Context, _ *mcp.CallToolRequest, in updateFileIn) (*mcp.CallToolResult, any, error) {
	form := url.Values{}
	if in.Name != nil {
		form.Set("name", *in.Name)
	}
	if in.Folder != nil {
		form.Set("folder_id", *in.Folder)
	}
	if len(form) == 0 {
		return nil, nil, errors.New("Give a new name or folder.")
	}
	var f projectFile
	if err := t.api.post(ctx, ws(in.Workspace, "projects", in.Project, "files", in.FileID), form, &f); err != nil {
		return nil, nil, err
	}
	return result(t.fileOut(f))
}

func (t *tools) deleteProjectFile(ctx context.Context, _ *mcp.CallToolRequest, in fileIDIn) (*mcp.CallToolResult, any, error) {
	if err := t.api.del(ctx, ws(in.Workspace, "projects", in.Project, "files", in.FileID), nil); err != nil {
		return nil, nil, err
	}
	return result(map[string]string{"deleted": in.FileID})
}

func (t *tools) createFileFolder(ctx context.Context, _ *mcp.CallToolRequest, in createFolderIn) (*mcp.CallToolResult, any, error) {
	var d fileFolder
	form := url.Values{"name": {in.Name}, "parent_id": {in.Parent}}
	if err := t.api.post(ctx, ws(in.Workspace, "projects", in.Project, "folders"), form, &d); err != nil {
		return nil, nil, err
	}
	return result(folderOutOf(d))
}

func (t *tools) updateFileFolder(ctx context.Context, _ *mcp.CallToolRequest, in updateFolderIn) (*mcp.CallToolResult, any, error) {
	form := url.Values{}
	if in.Name != nil {
		form.Set("name", *in.Name)
	}
	if in.Parent != nil {
		form.Set("parent_id", *in.Parent)
	}
	if len(form) == 0 {
		return nil, nil, errors.New("Give a new name or parent.")
	}
	var d fileFolder
	if err := t.api.post(ctx, ws(in.Workspace, "projects", in.Project, "folders", in.FolderID), form, &d); err != nil {
		return nil, nil, err
	}
	return result(folderOutOf(d))
}

func (t *tools) deleteFileFolder(ctx context.Context, _ *mcp.CallToolRequest, in folderIDIn) (*mcp.CallToolResult, any, error) {
	if err := t.api.del(ctx, ws(in.Workspace, "projects", in.Project, "folders", in.FolderID), nil); err != nil {
		return nil, nil, err
	}
	return result(map[string]string{"deleted": in.FolderID})
}

func (t *tools) listIssueAttachments(ctx context.Context, _ *mcp.CallToolRequest, in issueIn) (*mcp.CallToolResult, any, error) {
	id, key, err := t.issueRef(ctx, in.Workspace, in.Identifier)
	if err != nil {
		return nil, nil, err
	}
	var l fileList
	if err := t.api.get(ctx, ws(in.Workspace, "projects", key, "files"), url.Values{"issue": {id}}, &l); err != nil {
		return nil, nil, err
	}
	files := make([]fileOut, 0, len(l.Files))
	for _, f := range l.Files {
		files = append(files, t.fileOut(f))
	}
	return result(files)
}

func (t *tools) attachFile(ctx context.Context, _ *mcp.CallToolRequest, in attachIn) (*mcp.CallToolResult, any, error) {
	data, err := decodeUpload(in.Filename, in.Content)
	if err != nil {
		return nil, nil, err
	}
	id, key, err := t.issueRef(ctx, in.Workspace, in.Identifier)
	if err != nil {
		return nil, nil, err
	}
	var f struct {
		ID          string `json:"id"`
		URL         string `json:"url"`
		Name        string `json:"name"`
		ContentType string `json:"content_type"`
		Size        int64  `json:"size"`
	}
	fields := url.Values{"project": {key}, "issue": {id}}
	if err := t.api.upload(ctx, ws(in.Workspace, "files"), fields, in.Filename, data, &f); err != nil {
		return nil, nil, err
	}
	return result(fileOut{ID: f.ID, Name: f.Name, ContentType: f.ContentType, Size: f.Size, URL: t.app + f.URL, CanEdit: true})
}

func (t *tools) removeAttachment(ctx context.Context, _ *mcp.CallToolRequest, in attachmentIn) (*mcp.CallToolResult, any, error) {
	_, key, err := t.issueRef(ctx, in.Workspace, in.Identifier)
	if err != nil {
		return nil, nil, err
	}
	if err := t.api.del(ctx, ws(in.Workspace, "projects", key, "files", in.FileID), nil); err != nil {
		return nil, nil, err
	}
	return result(map[string]string{"removed": in.FileID})
}

// readFile fetches a Taskwell file with the caller's token. Only /api/files paths, so it can't be pointed elsewhere.
func (t *tools) readFile(ctx context.Context, _ *mcp.CallToolRequest, in readFileIn) (*mcp.CallToolResult, any, error) {
	path := strings.TrimPrefix(in.URL, t.app)
	if !strings.HasPrefix(path, "/api/files/") || strings.Contains(path, "..") {
		return nil, nil, fmt.Errorf("Not a Taskwell file url: %s", in.URL)
	}
	// ponytail: a video is downloaded only to report its size; a HEAD on the API would skip that.
	data, mime, err := t.api.file(ctx, path, maxUpload)
	if err != nil {
		return nil, nil, err
	}
	if strings.HasPrefix(mime, "image/") {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.ImageContent{Data: data, MIMEType: mime}}}, nil, nil
	}
	return result(map[string]any{"url": t.app + path, "content_type": mime, "size": len(data)})
}
