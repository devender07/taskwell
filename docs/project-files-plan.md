# Project files plan

Scope: each project gets a **Files** tab, a place to keep files on their own and not only inside an issue. People can:
- upload files, create folders, and move, rename, download or delete them;
- see a list of the attachments uploaded from that project's issues and comments.

Workspace settings show how much storage the workspace uses.

Out of scope:
- File versions or history, and comments on files.
- Workspace-wide files outside any project.
- Sharing a file by public link.
- In-browser previews beyond images (PDFs and other types open in a new tab or download).
- Thumbnails.
- A new quota. The existing `upload_quota_mb` limit (default 5120) already applies to every upload; this feature shows
  usage against it.

## Behaviour

- **Who sees files:** anyone who can see the project (`can_see_project`), guests included, the same rule issues follow.
  Private projects keep their files private.
- **Who changes them:**
  - People with `comments.create` (the right to attach to issues) can upload and create folders.
  - The uploader can rename, move and delete their own files and folders.
  - People with `projects.manage` can do this for anyone's.
- **Folders:**
  - Nested, with names unique within a parent (case-insensitive), 1 to 100 characters.
  - Only an empty folder can be deleted ("Move or delete what's inside first.").
  - A folder can't be moved into itself or into its own subfolder.
- **Attachments:** files uploaded from an issue's description or comments in the project appear under a fixed
  **Attachments** entry at the root.
  - They're read-only there: download only, and they're deleted with their issue.
  - Only uploads made after this ships are linked. Older attachments carry no project.
- **Limits:**
  - The existing `upload_max_mb` per file and `upload_quota_mb` per workspace apply to Files uploads too.
  - Several files can be uploaded at once; each counts separately.
- **Deleting a file** removes its row, then its stored bytes in S3 after the transaction commits. Deleting a project
  deletes its folders and files. The existing sweeper already removes S3 objects with no row.

## Schema (`backend/user/internal/db/migrations/024_project_files.sql`)

```sql
create table file_folders (
  id uuid primary key default uuidv7(),
  project_id uuid not null references projects(id) on delete cascade,
  parent_id uuid references file_folders(id) on delete cascade,
  name text not null check (length(name) between 1 and 100),
  created_by uuid references users(id) on delete set null,
  created_at timestamptz not null default now()
);
create unique index file_folders_name on file_folders (project_id, parent_id, lower(name)) nulls not distinct;

alter table files
  add column project_id uuid references projects(id) on delete cascade,  -- a project's file or attachment
  add column folder_id uuid references file_folders(id) on delete restrict,
  add column listed boolean not null default false;  -- uploaded through Files (true) vs an issue attachment (false)
create index on files (project_id, folder_id) where project_id is not null;
```

Reading a file (`GET /api/files/{id}/{name}`) changes from "a member of its workspace" to "a member of its workspace
and, when it has a project, able to see that project". That closes the gap where a guest could open another project's
attachment by its link.

## API

All of these are under `/api/workspaces/{slug}/projects/{key}`. They need a signed-in member who can see the project;
anyone else gets 404 `"Project not found."`

| Method & path | Request | Success | Errors |
|---|---|---|---|
| `GET /files` | `?folder=<id>` (omit for the root, `attachments` for the attachments list), `?q=` searches names across the whole project | `200 {"folder": Folder\|null, "path": [{"id","name"}], "folders": [Folder], "files": [File]}`: folders by name, then files newest first | 404 `"Folder not found."` |
| `POST /files` | multipart `file`, optional `folder_id` | `201 File` | 403 without `comments.create`; 413 too big or quota (existing messages); 404 folder |
| `POST /files/{id}` | FormData `name` and/or `folder_id` (`""` = root) | `200 File` | 403 `"You can only change your own files."`; 400 `{"errors": {"name": …}}`; 404 |
| `DELETE /files/{id}` | | `204` | 403 as above; 404; attachments (`listed = false`) return 404 |
| `POST /folders` | FormData `name`, optional `parent_id` | `201 Folder` | 400 `{"errors": {"name": "A folder with this name already exists."}}` |
| `POST /folders/{id}` | FormData `name` and/or `parent_id` | `200 Folder` | 400 `{"errors": {"parent_id": "A folder can't go inside itself."}}`; 403; 404 |
| `DELETE /folders/{id}` | | `204` | 409 `"Move or delete what's inside first."`; 403; 404 |

Shapes:
- **Folder:** `{"id", "name", "parent_id", "created_by": {"id","name"}|null, "created_at", "can_edit"}`.
- **File:** `{"id", "name", "content_type", "size", "uploader": {"id","name"}|null, "created_at", "url": "/api/files/<id>/<name>", "can_edit"}`.

The two existing endpoints change slightly:
- `POST /api/workspaces/{slug}/files` (editor uploads) takes an optional FormData `project` (the project key). The file
  is then linked to that project as an attachment. The key must be a project the user can see, else 404.
- **New:** `GET /api/workspaces/{slug}/storage` returns `200 {"used_bytes", "quota_bytes"}` to any member.

Realtime: every change publishes `/workspaces/{slug}/projects/{key}/files` and `/workspaces/{slug}/storage` to whoever
can see the project.

## Backend

- **Package:** a new `internal/projectfile` (routes, controller, model), using `storage.Store` for bytes.
- **Uploads:** reuse `file` package helpers for detecting the content type, cleaning names and checking limits; export
  what's needed rather than copying it.
- **Read access:** `file.find` gains the project visibility rule.
- **Tests:**
  - Visibility: a guest and a private project; a non-member gets 404.
  - Permissions: the uploader vs. someone else vs. `projects.manage`.
  - Folders: unique names, refusing to move into itself, refusing to delete a non-empty folder.
  - Moving and renaming; attachments read-only and listed under `attachments`; quota still enforced; search.
  - Deleting a project removes its files' rows.

## Frontend (user app)

- **Route:** `routes/_app/$workspace/_main/projects/$key_/files.tsx` at `/$workspace/projects/$key/files`, with
  `?folder=` and `?q=` in the URL.
  - The project page gets a "Files" link next to "Cycles".
  - The command palette gets "Go to files" while a project is open.
- **Page:**
  - The sticky header holds a breadcrumb (Files › Design › Mockups), search, "New folder" and "Upload". Dropping files
    onto the page also uploads them.
  - A list shows folders first, then files: icon, name, size, uploader, date, and a "More" menu with Download, Rename,
    Move to…, Delete (only when `can_edit`).
  - Image rows show a small inline preview; clicking an image opens it full size, other files open the `url` in a new
    tab.
  - Upload progress shows per file, with errors from the API.
  - The Attachments entry at the root lists the attachments, read-only.
  - The empty state reads "Upload files or create a folder to keep this project's files together."
- **Editor uploads:** they pass `project` when the editor belongs to a project's issue or comment. `uploadFile` in
  `api.ts` gains an optional project key.
- **Settings → Workspace:** "Storage: 1.2 GB of 5 GB used" with a bar.
- **Icons:** any new icons (folder, upload) go in `@taskwell/shared/icons`.

Sentence case everywhere.

## Built differently

- **Sweeper:** the sweeper skips `listed` files. It deletes unreferenced files after a day, which would otherwise
  have removed every Files upload.
- **Download name after a rename:** SeaweedFS ignores header changes on copy and on presigned links, so the object is
  stored again after a rename. This is marked `ponytail:`; S3 CopyObject is the upgrade path.
- **Upload progress:** each upload shows "Uploading…" until it finishes or fails, with no percentage. Real progress
  needs XMLHttpRequest, which would bypass the token refresh.
- **Opening a file:** clicking a file opens its `url` in a new tab, with no full-size viewer in the page.
- **Archived projects:** read-only. Uploads, new folders, renames, moves and deletes return 403 "This project is archived.", `can_edit` is false, and editor uploads linked to the project return 404.
- **Images and videos only:** every upload (Files, issue and comment attachments) must sniff as PNG, JPEG, GIF, WebP,
  MP4 or WebM, else `400 {"errors": {"file": "Only images (PNG, JPEG, GIF, WebP) and videos (MP4, WebM) can be
  uploaded."}}`. Videos are served inline like images.
- **Deleting attachments:** the uploader or `projects.manage` can delete an attachment from the Attachments list
  (`DELETE /files/{id}`); its link in the issue or comment then breaks. Rename and move stay Files-only.
- **Issue files** (migration 025, `files.issue_id`): on the issue page the description's attach button, paste and drop
  upload to the issue (`POST /api/workspaces/{slug}/files` with `project` and `issue` = the issue's id) instead of
  inserting into the description. They show under the description as thumbnails (six, then "+N more"), with "View
  all" opening a dialog to download or delete. Listed by `GET /projects/{key}/files?issue=<id>`; they're attachments
  too, the sweep keeps them, and they go when the issue is purged. Comments and the create form still insert inline.
- **Workspace file manager:** `/$workspace/files` (sidebar "Files") lists the projects as folders with the storage bar.

## Verification

- `go vet`, and `go test` for `internal/projectfile` and `internal/file`.
- `npm run build`, `npm run check`.
- Integration:
  1. Upload several files and a folder, then rename, move and delete them.
  2. Attach an image in an issue and see it under Attachments.
  3. A guest without the project gets 404 on its file links.
  4. The storage bar goes up and down.
  5. Uploading past the quota shows its message.
