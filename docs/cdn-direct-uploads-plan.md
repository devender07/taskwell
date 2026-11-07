# Direct uploads and file CDN plan

Two changes to how uploads move:

1. **Direct uploads.** The browser PUTs the bytes straight to the bucket (R2 in production, SeaweedFS in dev) with a
   presigned URL, then tells the API the upload is done. The API never streams the file. API and MCP clients keep the
   existing multipart upload (`POST .../files` with a `file` part); only the user app switches.
2. **CDN serving.** `GET /api/files/{id}/{name}` still checks access, but when a CDN is configured it redirects to a
   Cloudflare Worker (`files.taskwell.dev`) with a signed, day-stable link instead of a 1-hour presigned R2 link. The
   Worker checks the signature (no call back to the API) and serves the object from Cloudflare's edge cache, filling
   it from R2 through a bucket binding. Without a CDN configured (dev), serving works as today.

Out of scope: upload progress bars, resumable/multipart uploads for big files, purging the edge cache on delete,
moving AI test screenshots or MCP uploads to direct PUT.

## Facts checked before planning

- SeaweedFS (dev) answers a CORS preflight only after `PutBucketCors`; with it, the preflight passes.
- SeaweedFS does **not** enforce the signed `Content-Length` or `Content-Type` of a presigned PUT. So the API checks
  every upload after it lands (size from `HeadObject`, type sniffed from the first 512 bytes), whatever the store.

## Schema: migration `028_direct_uploads.sql`

```sql
-- A presigned upload the browser hasn't finished yet. The object key is the id; finishing moves it to files.
create table uploads (
    id uuid default uuidv7() primary key,
    workspace_id uuid references workspaces (id) on delete cascade, -- null: an avatar
    user_id uuid not null references users (id) on delete cascade,
    name text not null,
    content_type text not null,
    size bigint not null,
    created_at timestamptz default now() not null
);
create index uploads_created_at_idx on uploads (created_at);

insert into system_config (key, value, secret, description) values
  ('files_cdn_url', '', false, 'Where the file Worker serves uploads (https://files.taskwell.dev); empty redirects to presigned bucket links'),
  ('files_cdn_secret', '', true, 'Signs file links for the Worker; the same value as its FILES_SECRET')
on conflict (key) do nothing;
```

An upload is live for **1 hour** (`uploadTTL`); the presigned PUT URL for **15 minutes**. The sweep deletes `uploads`
rows older than 1 hour; their objects (no `files` row) go with the existing orphan sweep after 24 hours.

Quota: a workspace's used bytes become `sum(files.size) + sum(uploads.size)` of live uploads, so parallel uploads
can't overshoot it (this also closes the existing `ponytail:` note's gap for direct uploads).

## Bucket CORS

On startup the user service calls `PutBucketCors` once, allowing `PUT` and `GET` from the origin of `app_base_url`
with any header, max age 1 hour. It logs a warning and carries on if the store refuses (an R2 token with only object
read/write can't set CORS: then set it once in the R2 dashboard or with `wrangler r2 bucket cors`, see
`deploy/README.md`).

## API contract

All bodies are `FormData`, responses JSON, errors in the existing shapes (`{"error": msg}` or
`{"errors": {"file": msg}}`). Messages reuse the current ones: `errChooseFile`, `errChooseImage`, `errChooseMedia`,
"Files must be N MB or smaller.", the out-of-storage message, "Images must be 2 MB or smaller.".

### Start an upload

`POST /api/workspaces/{slug}/uploads`: a workspace upload (editor images, issue attachments, project files).

| Field | Meaning |
| --- | --- |
| `name` | the file's name (cleaned with `CleanName`) |
| `size` | bytes, integer > 0 |
| `type` | the browser's MIME type; must be one of the media types (`image/png`, `image/jpeg`, `image/gif`, `image/webp`, `video/mp4`, `video/webm`) |

- `201` `{"id": "<uuid>", "url": "<presigned PUT>", "headers": {"Content-Type": "...", "Content-Disposition": "...", "Cache-Control": "..."}}`.
  The browser PUTs the file to `url` with exactly `headers` (no credentials).
- `400` `{"errors": {"file": errChooseFile}}` (no name, size ≤ 0 or not a number), `{"errors": {"file": errChooseMedia}}` (type not allowed).
- `413` `{"errors": {"file": "Files must be N MB or smaller."}}` or the out-of-storage message.
- `404` `{"error": "Workspace not found."}`.

`POST /api/me/uploads`: an avatar. Same fields and response; type must be an image, size ≤ 2 MB
(`413` "Images must be 2 MB or smaller.", `400` errChooseImage). `workspace_id` is null.

### Finish an upload

The existing upload endpoints take `file_id` (the upload's id) in place of the multipart `file` part. Everything else
about them (fields, permissions, responses, realtime events) is unchanged:

- `POST /api/workspaces/{slug}/files` with `file_id`, optional `project`, `issue` → `201` UploadedFile
- `POST /api/workspaces/{slug}/projects/{key}/files` with `file_id`, optional `folder_id` → `201` ProjectFile
- `POST /api/me/avatar` with `file_id` → `200` the user

Finishing checks, in order:
1. A live `uploads` row with that id, this user, and this workspace (null for the avatar endpoint); else `400`
   `{"errors": {"file": "Upload not found. Try again."}}`.
2. `HeadObject`: the object exists and its size equals the declared size; else `400` same message.
3. The first 512 bytes (ranged `GetObject`) sniff (`http.DetectContentType`) to the declared type, and it's a media
   type (an image for avatars); else delete the object, `400` errChooseMedia (errChooseImage for avatars).
4. One transaction: insert the `files` row **with the upload's id** (the object key), apply the placement, delete the
   `uploads` row, publish the same realtime event as today.

A request with a multipart `file` part keeps the old server-side path (API, MCP). One with neither gets `400`
errChooseFile.

### Serve

`GET /api/files/{id}/{name}` (unchanged access check). When both `files_cdn_url` and `files_cdn_secret` are set:

- `302` to `{files_cdn_url}/{id}/{escapedName}?exp={exp}&sig={sig}`, where `escapedName` is `url.PathEscape(name)`
  (as in `file.URL`), `exp` is a Unix time: the start of the next UTC day plus one day (so a link is the same all day
  and works for at least 24 hours), and
  `sig = base64url-no-padding(HMAC-SHA256(files_cdn_secret, "/" + id + "/" + escapedName + "\n" + exp))`.
- `Cache-Control: private, max-age=<seconds until the next UTC day>` on the redirect.

Otherwise the presigned-GET redirect stays as it is. Avatars still redirect to `/api/files/{id}/avatar`.

Test vector (both the Go test and the Worker's check use it):
secret `test-secret`, path `/0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b/photo%201.png`, exp `1767312000`
→ `sig` `7EzqQ3f5boRpA14Ux8-zYIfFyL4IfxF2bb2GA55QiUQ` (computed with Python's `hmac`; the Go test and the Worker check
both assert it).

## The Worker (`cdn/`, at the repo root)

Plain JavaScript module, no dependencies, deployed with `npx wrangler deploy` from `cdn/`.

- `wrangler.toml`: name `taskwell-files`, R2 binding `BUCKET` → bucket `taskwell`, custom domain `files.taskwell.dev`.
  Secret `FILES_SECRET` (`npx wrangler secret put FILES_SECRET`), the same value as `files_cdn_secret`.
- `GET`/`HEAD /{uuid}/{name}?exp&sig`: `403` unless `exp` is in the future and `sig` verifies
  (`crypto.subtle.verify`, constant time); `404` for a missing object. No-store on errors.
- Edge cache key `https://{host}/{id}` (the object, shared by every user and link). Miss: `BUCKET.get(id)`, store in
  `caches.default` with `Cache-Control: public, max-age=31536000, immutable` (objects never change: a new upload is a
  new id). Ranges (video) are answered from the cached copy.
- Response to the browser: the object's `Content-Type`, `ETag`, `Content-Disposition` (inline for the media types,
  else `attachment; filename*=UTF-8''{escapedName}`), `Cache-Control: private, max-age={exp - now}`,
  `X-Content-Type-Options: nosniff`, `Content-Security-Policy: default-src 'none'; sandbox`,
  `Cross-Origin-Resource-Policy: cross-origin`.
- ponytail: a deleted file stays in the edge cache, reachable only through links issued before the delete, until they
  expire (at most 2 days). Purge from the API on delete if that ever matters.
- `cdn/worker.check.mjs` (`node cdn/worker.check.mjs`): the signing test vector; a changed path, exp, secret or an
  expired link rejected; the Worker against an in-memory bucket and edge cache (headers, edge fill and hit, ranges,
  downloads, 403/404/405).

## Frontend (user app)

- `src/api.ts`: one helper, `directUpload(startPath, file)`: POSTs `name`, `size`, `type` to `startPath`, PUTs the file
  to the returned `url` with the returned `headers` (`fetch`, `credentials: 'omit'`), returns the id. A failed PUT
  throws `Error("Upload failed. Try again.")`; a start error throws the server's `errors.file`/`error` as today.
- `uploadFile` and `uploadProjectFile` call it (`/workspaces/{slug}/uploads`), then POST their endpoint with `file_id`
  plus the same fields as now. Their signatures and return types don't change, so the editor, issue files, files page
  and command palette need no edits.
- Avatar form: `directUpload('/me/uploads', file)`, then `POST /me/avatar` with `file_id`.
- `vite.config.ts` CSP: `connect-src` adds `VITE_UPLOAD_ORIGIN` (the bucket's browser-facing origin; set in
  `.env.production` to `https://3ad7af564ae058bccfaf91aaa3f72eae.r2.cloudflarestorage.com`); `img-src`/`media-src`
  already allow any https origin, which covers `files.taskwell.dev`.

## Verification

- Backend: `go vet`, `go test` (sign test vector, start-upload validation, finish checks against the dev store),
  curl the whole flow against the running API with the dev store: start → PUT with curl → finish; a PUT of the wrong
  bytes is rejected at finish and its object deleted; quota counts live uploads.
- Frontend: `npm run build`; then in the main session, upload an editor image, an issue attachment, a project file and
  an avatar in the browser against `make`.
- Worker: `node cdn/worker.check.mjs`; `npx wrangler dev` against a local R2 if time allows.
