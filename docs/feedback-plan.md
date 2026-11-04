# Customer feedback plan

Scope: a workspace collects what its customers ask for in one **Feedback** inbox. Feedback arrives from a public form
(a per-workspace link the team shares with customers) or is added by a member (pasted from a call, email or ticket).
Members triage it: link it to an existing issue, create an issue from it, or archive it. An issue shows the feedback
linked to it, so the team sees how many customers asked and who.

Out of scope: inbound email address, widgets/embeds, customer accounts or portal, votes, emailing customers when the
issue ships, email/in-app notifications for new feedback (the sidebar count covers it), rich text feedback,
attachments, one feedback linked to several issues, feedback analytics.

## Schema

Migration `backend/user/internal/db/migrations/006_feedback.sql`:

```sql
create table feedback (
  id           uuid primary key default uuidv7(),
  workspace_id uuid not null references workspaces on delete cascade,
  body         text not null check (char_length(body) between 1 and 5000),
  email        text check (char_length(email) <= 254),
  name         text check (char_length(name) <= 80),
  source       text not null check (source in ('form', 'member')),
  issue_id     uuid references issues on delete set null,
  archived_at  timestamptz,
  created_by   uuid references users on delete set null,  -- null for the public form
  created_at   timestamptz not null default now()
);
create index on feedback (workspace_id, id desc);
create index on feedback (issue_id) where issue_id is not null;

-- The public form's link token; null while the form is off.
alter table workspaces add column feedback_token text unique;
```

No `system_config` keys.

## Permissions

New `feedback.manage` (group "Feedback", label "Manage the feedback form, archive and delete feedback", active).
Every workspace member can read feedback, add it, and link/unlink it to issues they can see. `feedback.manage`
gates the form settings, archive/unarchive and delete. Admin has it through the catalog as usual.

## States

Computed, not stored: **new** = not archived and no issue; **linked** = not archived and has an issue; **archived** =
`archived_at` set. Linking new feedback moves it to linked; unlinking moves it back to new.

## API contract

FormData in, JSON out. Non-members get 404 `{"error":"Workspace not found."}` as today. Missing permission: 403
`{"error":"You don't have permission to do that."}`. Unknown feedback id (or not a UUID): 404
`{"error":"Feedback not found."}`.

**Feedback JSON**

```json
{
  "id": "…", "body": "Plain text, newlines kept", "email": "ana@acme.com" | null, "name": "Ana" | null,
  "source": "form" | "member",
  "issue": {"identifier": "ENG-12", "title": "…", "status": {"name","color","category"}} | null,
  "archived": false,
  "created_by": {"id","name"} | null,
  "created_at": "RFC3339 UTC"
}
```

`issue` is null when unlinked, and also when the linked issue is in a private project the caller can't see
(`can_see_project`), so a linked item still counts as linked but shows no issue.

| Method & path | Fields | Success | Failures |
|---|---|---|---|
| `GET /api/workspaces/{slug}/feedback?state=new\|linked\|archived&q=&before=` | — | `200 {"items":[feedback…],"has_more":bool}` newest first, 50 per page; `state` defaults to `new`; `q` matches body, email, name (ILIKE); `before` a feedback id cursor | 400 `{"error":"Invalid cursor."}` |
| `GET /api/workspaces/{slug}/feedback/count` | — | `200 {"new": n}` | |
| `POST /api/workspaces/{slug}/feedback` | `body`, `email` (optional), `name` (optional) | `201 feedback` (`source` "member") | 400 `{"errors":{…}}` (below) |
| `POST /api/workspaces/{slug}/feedback/{id}` | `issue` (identifier like `ENG-12`, or empty to unlink) and/or `archived` (`true`/`false`); only the fields sent change | `200 feedback` | 400 `{"errors":{"issue":"Issue not found."}}` (unknown or not visible); 403 for `archived` without `feedback.manage`; 404 |
| `DELETE /api/workspaces/{slug}/feedback/{id}` | — | `204` | 403; 404 |
| `GET /api/workspaces/{slug}/issues/{identifier}/feedback` | — | `200 [feedback…]` newest first, archived included | 404 `{"error":"Issue not found."}` as today |
| `GET /api/workspaces/{slug}/feedback-form` | — | `200 {"enabled":bool,"token":string\|null}` | 403 without `feedback.manage` |
| `POST /api/workspaces/{slug}/feedback-form` | `enabled` (`true`/`false`), `rotate` (`true` makes a new token, old link stops working) | `200 {"enabled","token"}` | 403 |
| `GET /api/feedback-forms/{token}` (public, no auth) | — | `200 {"workspace":"Acme"}` | 404 `{"error":"This feedback form isn't available."}` (unknown token or form off) |
| `POST /api/feedback-forms/{token}` (public, no auth) | `email`, `name` (optional), `body`, `website` (honeypot) | `201 {}` | 400 `{"errors":{…}}`; 404 as above; 429 `{"error":"Too many attempts. Try again later."}` |

Validation messages (trim first):
- `body`: `"Write the feedback."` when empty, `"Feedback must be 5,000 characters or fewer."`
- `email`: required on the public form (`"Enter your email."`), optional for members; `"Enter a valid email."` (same
  check the auth package uses), max 254.
- `name`: `"Name must be 80 characters or fewer."`
- Honeypot `website` non-empty: answer `201 {}` and store nothing.

Token: 32 random bytes base64url (as `gitlink.randomToken`). Public POST is rate limited per client IP (10 per hour
burst, using the existing Postgres `rate_limits` limiter in `auth`; export or move it to `web` rather than copy it).
Public endpoints don't need CSRF (no cookies used) and are mounted without the auth wrapper.

**Realtime.** New helper `realtime.Feedback(slug)` = `/workspaces/{slug}/feedback`. Every feedback write publishes
kind `feedback.changed` with paths `…/feedback`, `…/feedback/count`, and for links/unlinks the affected issues'
`…/issues/{identifier}/feedback`. Audience: the workspace.

## Frontend (user app)

- `src/api.ts`: `Feedback`, `FeedbackPage`, `FeedbackForm` types.
- **Sidebar**: "Feedback" after Inbox with a new `FeedbackIcon` in `@taskwell/shared/icons` and the `new` count as a
  pill (from `…/feedback/count`, live). Command palette: "Go to Feedback", "Add feedback".
- **Feedback page** `routes/_app/$workspace/_main/feedback.tsx` (`/$workspace/feedback`), parts in
  `components/feedback/`:
  - `validateSearch`: `state: 'new' | 'linked' | 'archived'` (default `new`), `q`.
  - Header: title "Feedback", tabs New / Linked / Archived (`@taskwell/shared/tabs`), search input, "Add feedback" button.
  - List of cards: who (name or email, else "Added by {member}"), relative time, source badge ("Form"), body
    (pre-wrap, clamped to 6 lines with "Show more"), linked issue chip (identifier + title, links to the issue).
  - Actions per card: **Link to issue** (popover with issue search, the same search `relations.tsx` uses),
    **Create issue** (`createPrefilled` from `components/app/shortcuts.tsx` with title = first line of the body up to
    80 chars and description = the body as a paragraph doc, then links the feedback to the created issue; extend
    `Prefill` with an `onCreated(identifier)` or a `feedback` id, whichever is smaller), **Unlink**, **Archive** /
    **Unarchive** and **Delete** (confirm dialog) with `feedback.manage`.
  - Loads 50, next page when the end scrolls into view; empty states per tab ("No new feedback", with a pointer to
    Settings → Feedback form on New).
- **Add feedback dialog**: Message (textarea), Customer email, Customer name. `noValidate`, messages from
  `@taskwell/shared/validate` (add a `feedbackBody` validator mirroring the API).
- **Issue page**: a "Customer feedback" section (count in the heading, then each item's who, time and body clamped
  to 3 lines), shown only when there is any; live on `…/issues/{identifier}/feedback`.
- **Settings → Feedback form** (`routes/_app/$workspace/_main/settings/feedback.tsx`, nav item shown with
  `feedback.manage`): on/off checkbox, the link (`{origin}/f/{token}`) with Copy, "Create a new link" (confirm: the
  old one stops working).
- **Public form** `routes/f.$token.tsx` (`/f/$token`, outside `_app`, no sign-in): Taskwell logo, "Send feedback to
  {workspace}", fields Email, Name (optional), Message, hidden honeypot `website`, "Send feedback" button; on success
  "Thanks, {workspace} got your feedback." with "Send more". 404 state: "This feedback form isn't available."

## Agents

- **Backend**: migration, new `internal/feedback` package (routes/controller/model/validate), permission in
  `workspace/permissions.go`, issue feedback endpoint (in `feedback`, resolving the issue with visibility checks),
  public endpoints and rate limit, realtime helper, wiring in `cmd/user/main.go`. Tests: validation, states and
  paging, link/unlink with private-project visibility, permission checks, form on/off/rotate, public submit with
  honeypot and form off. `go vet`, `go test` with `TEST_DATABASE_URL`, curl against the running API.
- **Frontend**: everything under Frontend. `npm run build`, `npm run check`.
