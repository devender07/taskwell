# Attachments and avatars plan (Phase 4 + the Phase 1 leftover)

Scope: upload images and files into descriptions and comments (images show inline, other files as a link with the file name), and profile avatars. Files are stored on local disk. No new dependencies.

Out of scope: S3 storage, image resizing/thumbnails (needs `golang.org/x/image`), deleting orphaned uploads, previews for PDFs/videos, per-file permissions finer than "member of the workspace".

## Storage

- Directory from env `UPLOAD_DIR` (default `./uploads`, created at startup with 0700). Each file is stored as `<UPLOAD_DIR>/<id>` (the uuid only; never the user's file name). `ponytail:` local disk; move behind an interface with S3 when running more than one API host (the multi-instance live editing already works, files don't).
- `system_config` key `upload_max_mb` (default `10`), seeded by the migration.

Migration `backend/user/internal/db/migrations/015_attachments.sql`:

```sql
create table files (
  id           uuid primary key default uuidv7(),
  workspace_id uuid references workspaces on delete cascade, -- null for avatars
  uploader_id  uuid references users on delete set null,
  name         text not null,       -- original file name, cleaned (no path parts, control chars removed, max 200 chars)
  content_type text not null,       -- from http.DetectContentType on the first 512 bytes, not the client's header
  size         bigint not null,
  created_at   timestamptz not null default now()
);
alter table users add column avatar_file_id uuid references files on delete set null;
insert into system_config (key, value) values ('upload_max_mb', '10') on conflict do nothing;
```

(Check how `system_config` rows are seeded elsewhere and follow that, incl. any description/secret column.)

Deleting a workspace deletes its file rows; the files on disk stay (`ponytail:` orphan sweep later).

## API contract

| Method & path | Needs | Fields | Success | Failures |
|---|---|---|---|---|
| `POST /api/workspaces/{slug}/files` | member | multipart `file` | `201 {"id","url":"/api/files/{id}/{name, path-escaped}","name","content_type","size","image": bool}` | 400 `{"errors":{"file":"Choose a file."}}`; 413 `{"errors":{"file":"Files must be 10 MB or smaller."}}` (number from config) |
| `GET /api/files/{id}/{name}` | member of the file's workspace (avatars: any signed-in user) | — | `200` file bytes. Headers: `Content-Type` stored type; `X-Content-Type-Options: nosniff`; `Content-Security-Policy: sandbox; default-src 'none'`; `Content-Disposition: inline` for `image` types, `attachment; filename*=UTF-8''…` for everything else; `Cache-Control: private, max-age=31536000, immutable` (content never changes for an id) | 401; 404 `{"error":"File not found."}` (also for non-members). `{name}` in the path is ignored for lookup |
| `POST /api/me/avatar` | signed in | multipart `file` (PNG, JPEG, GIF or WebP by sniffed type; max 2 MB) | `200 user` (the `GET /api/me` JSON) | 400 `{"errors":{"file":"Choose a PNG, JPEG, GIF or WebP image."}}` / `"Choose a file."`; 413 `{"errors":{"file":"Images must be 2 MB or smaller."}}` |
| `DELETE /api/me/avatar` | signed in | — | `200 user` | — |
| `GET /api/users/{id}/avatar` | signed in | — | `302` to the avatar's `/api/files/{id}/avatar` URL, `Cache-Control: private, max-age=300`; `404` (same cache header) when the user has no avatar | 401 |

`image` = sniffed type is `image/png`, `image/jpeg`, `image/gif` or `image/webp` (never SVG). `GET /api/me` gains `"avatar_url": string|null` (the file URL).

### Documents

`checkDoc` (backend) and the editor gain:
- node `image`: `attrs.src` must match `^/api/files/<uuid>/[^/?#]+$`; `attrs.alt` optional string (≤ 200). Block-level node (in `doc`, `blockquote`, `listItem`).
- `link` `href` may also be a file URL matching that same pattern.

## Frontend

- **Shared `Avatar`**: gains optional `src`; when set it renders `<img>` (object-fit cover, same size/radius) and falls back to initials on `onError`. Add a helper `avatarSrc(userId)` in the user app → `/api/users/{id}/avatar`. Use it for every avatar of a person in the user app (navbar, members, assignees in lists/board/properties, comments/timeline). Own avatar after upload: append `?v=<Date.now()>` held in state so it refreshes immediately.
- **Settings → Profile**: "Avatar" section with the current avatar (64px), "Upload image" (hidden file input, `accept="image/png,image/jpeg,image/gif,image/webp"`), "Remove", errors under it.
- **RichEditor**:
  - Custom `image` node (Tiptap `Node.create` from `@tiptap/core`/`@tiptap/react`, no new package): renders `<img src alt>` max-width 100%, 8px radius; selected state outline.
  - New optional prop `upload?: (file: File) => Promise<{ url: string; name: string; image: boolean }>`; when set: toolbar button "Attach file" (paperclip, opens a file picker, multiple), paste and drop of files. While uploading, insert nothing and show a caption "Uploading name…" under the editor; on success insert an image node (images) or a link with the file name as text (other files); on failure show the error caption ("Couldn't upload name: message").
  - Read-only: images render; file links open in a new tab (download).
- **User app**: `uploadFile(workspace, file)` in `api.ts` posting to `/files`; pass `upload` to the description editor, the create dialog and the comment composer/edit.
- Vite proxy already covers `/api`.

## Backend

- Package `internal/file` (routes/controller/model) for upload/serve and avatar endpoints; `checkDoc` changes in `internal/issue/doc.go` (+ table test rows: image ok, image with external src rejected, file link ok, `/api/files/../x` rejected).
- Upload handling: `http.MaxBytesReader` at limit + 1 MB of form overhead; stream to a temp file in `UPLOAD_DIR`, then rename to the id (no full read into memory); clean the name; sniff type.
- Serving: `http.ServeContent` from the file on disk (range requests for free).
- Verify with curl: upload (image and a .txt), fetch as member, fetch as non-member (404), signed out (401), oversize (413), headers above; avatar upload/replace/remove, wrong type, `GET /api/users/{id}/avatar` 302/404; a description with an image saves and one with an external image is rejected.

## Verification (integration)

Paste/drop and the attach button in the description (live session) and a comment; image shows for another member; a .txt downloads; avatar upload shows in the navbar and member list; remove falls back to initials; iPad file picker works (headless: `setInputFiles`).
