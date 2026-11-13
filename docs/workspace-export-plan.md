# Workspace export plan

One click in Settings → Workspace downloads a zip of the workspace's data: what a team needs to leave, to answer a
data request (GDPR), or to keep its own backup. Admins only by default.

Out of scope: scheduled or emailed exports, importing an export back (the CSV inside is importable by the existing
CSV import), chats and DMs, notes, API tokens, webhook and integration secrets, personal settings.

## Permission

`workspace.export` ("Export workspace data"), group Workspace (so guests can never hold it), in
`workspace/permissions.go` (already added as `PermWorkspaceExport`). Admin has it; custom roles opt in.

## No schema change

No migration. No `system_config` keys.

## API contract

Two steps, so errors come back as JSON and the download itself is a plain browser navigation that needs no cookie
(large zips never sit in the browser's memory).

### Start

`POST /api/workspaces/{slug}/export` (empty FormData)

- `200` `{"url": "/api/exports/<token>"}`: a path on the API host. The token is signed (HMAC, the service's JWT
  secret), names the workspace and the user, and expires in 10 minutes.
- `403` `{"error": "You don't have permission to export this workspace."}`
- `404` `{"error": "Workspace not found."}`
- `429` `{"error": "Too many exports. Try again in a few minutes."}` (3 starts per workspace per hour, the shared
  `web.Limiter`)

### Download

`GET /api/exports/{token}` (public route: the token is the credential; it still re-checks that the user is a member
with `workspace.export`)

- `200` `application/zip`, `Content-Disposition: attachment; filename="<slug>-export-<YYYY-MM-DD>.zip"`, streamed
  as it's built (archive/zip writing straight to the response), files streamed from storage one at a time.
- `404` `{"error": "This export link has expired. Start a new export."}` for a bad, expired or no-longer-allowed
  token.

`withTimeouts` in `cmd/user/main.go` gives paths starting with `/api/exports/` the 10-minute deadline uploads get.

## Zip contents

```
README.txt        what's inside, when, by whom
workspace.json    everything below, structured
issues.csv        one row per issue; columns the CSV import reads
comments.csv      one row per comment
files/<id>-<name> every file the exporter can see (cleaned name, as file.CleanName)
```

`workspace.json`:

```
{
  "exported_at", "exported_by": {"name", "email"},
  "workspace": {"name", "slug", "created_at"},
  "members": [{"name", "email", "role", "guest", "joined_at"}],
  "roles": [{"name", "builtin", "permissions"}],
  "statuses": [{"name", "category", "position"}],
  "labels": [{"name", "color"}],
  "projects": [{"key", "name", "description", "private", "archived", "start_date", "target_date",
                "milestones": [{"name", "date"}],
                "cycles": [{"number", "name", "starts_on", "ends_on"}],
                "updates": [{"health", "body_text", "body", "author", "created_at"}]}],
  "issues": [{"identifier", "project", "title", "description_text", "description", "status", "priority",
              "assignee", "created_by", "labels", "due_date", "cycle", "parent", "created_at", "updated_at",
              "comments": [{"author", "body_text", "body", "created_at", "edited_at"}],
              "files": ["files/<id>-<name>", ...]}],
  "feedback": [{"title", "body_text", "email", "status", "issue", "created_at"}],
  "files": [{"name", "content_type", "size", "project", "issue", "folder", "uploaded_by", "created_at", "path"}]
}
```

People are emails (`assignee`, `created_by`, `author`, `uploaded_by`), null when gone. `description`/`body` are the
stored rich-text JSON; `*_text` the plain text of it (reuse the existing doc-to-text helper if there is one, else a
small walker in the export package). Deleted (trashed) issues are left out.

`issues.csv` columns: `Identifier, Project, Title, Description, Status, Priority, Estimate, Assignee, Labels, Due date,
Cycle, Created`; issues in `workspace.json` carry `estimate` too. Starting an export records `workspace.exported` in
the audit log. Only files in a project are included (chat, DM and note uploads stay out), and feedback has no title. Title/Description/Status/Priority/Assignee/Labels/Due date match what the CSV import reads.
`comments.csv`: `Issue, Author, Created, Body`.

**Visibility:** only projects the exporter can see (`can_see_project`), so a private project they aren't in stays
out, with its issues, comments and files.

## Backend layout

New package `internal/export` (routes/controller/model, plus `zip.go` for writing). Read everything with a handful
of queries (no per-issue query loops). Files: stream each object from `storage.Store` (add a `Get(ctx, id)
(io.ReadCloser, error)` there if none exists). A file whose object is missing is skipped and listed in README.txt.

## Frontend

Settings → Workspace page, a new section after the existing ones, shown only with `workspace.export`:

- Heading "Export data", text "Download this workspace's projects, issues, comments and files as a zip of JSON and
  CSV. Private projects you're not in are left out."
- Button "Export workspace" → POST start (button shows "Preparing…"), then `location.assign(apiUrl(url))`; the
  browser shows the download. Errors from the start call show under the button.
- `src/api.ts`: `startExport(slug)` returning `{url}`.

## Verification

- `go vet`, `go test`: a test that builds an export of a small seeded workspace and checks the zip's entries,
  `workspace.json` shape, a private project left out, CSV header row, an expired/forged token refused.
- curl against a running API: start, download, unzip, inspect.
- Frontend: `npm run build`.
