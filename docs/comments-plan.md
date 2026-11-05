# Comments and activity plan (Phase 4)

Scope: comments on issues (rich text with the shared `RichEditor`, `@mentions` that email like descriptions), and an activity log of changes to an issue. Both show in one timeline under the issue, oldest first. Turns on the `comments.create` and `comments.manage` permissions.

Out of scope: threaded replies, reactions, comment attachments (attachments plan), live collaborative editing of comments, notifications other than mention emails (Phase 6 builds on the events recorded here).

## Schema

Migration `backend/user/internal/db/migrations/014_comments.sql` (013 is taken by multi-instance live editing):

```sql
create table comments (
  id         uuid primary key default uuidv7(),
  issue_id   uuid not null references issues on delete cascade,
  author_id  uuid references users on delete set null,
  body       jsonb not null,                 -- ProseMirror JSON, same rules as descriptions, never empty
  created_at timestamptz not null default now(),
  edited_at  timestamptz
);
create index on comments (issue_id, id);

create table issue_events (
  id         uuid primary key default uuidv7(),
  issue_id   uuid not null references issues on delete cascade,
  actor_id   uuid references users on delete set null,
  kind       text not null,
  data       jsonb not null default '{}',
  created_at timestamptz not null default now()
);
create index on issue_events (issue_id, id);
```

Event kinds and `data` (names are snapshots so the log reads right after renames/deletes):

| kind | data | shown as |
|---|---|---|
| `created` | `{}` | "Demo created the issue" |
| `title` | `{"from","to"}` | "changed the title to “to”" |
| `description` | `{}` | "edited the description" — at most one per actor per issue per 10 minutes (if the actor's latest event on the issue is a `description` event younger than 10 min, bump its `created_at` instead of inserting) |
| `status` | `{"from":{"id","name","category"},"to":{…}}` | "moved from Todo to In progress" (status icon) |
| `assignee` | `{"from":person\|null,"to":person\|null}` | "assigned to Maya" / "unassigned Maya" / "assigned to themselves" |
| `priority` | `{"from":0-4,"to":0-4}` | "set priority to High" / "removed priority" |
| `due_date` | `{"from":"YYYY-MM-DD"\|null,"to":…}` | "set the due date to Nov 3" / "removed the due date" |
| `labels` | `{"added":[{"id","name","color"}],"removed":[…]}` | "added Bug, removed Docs" |
| `parent` | `{"from":ref\|null,"to":ref\|null}` (ref = `{"id","identifier","title"}`) | "made this a sub-issue of ENG-1" / "removed the parent ENG-1" |
| `link` | `{"type":"blocks"\|"blocked_by","issue":ref,"removed":bool}` | "marked this as blocking ENG-3" / "removed the link to ENG-3" |

Events are written in the same transaction as the change, only when the value actually changes, by the issue model (create, update incl. board drag, link, unlink, live `save` of the description). Changing the project key/name doesn't create events.

## Permissions

`comments.create` and `comments.manage` become active (`Active: true` in `internal/workspace/permissions.go`; constants `PermCommentsCreate`, `PermCommentsManage`). Member role already has `comments.create` by default. Edit a comment: its author (who still has `comments.create`) or `comments.manage`. Delete: author or `comments.manage`. Commenting on issues in archived projects is not allowed (403 `"This project is archived."`, same as issue edits).

## API contract

Conventions as before (FormData in, JSON out; 401 `Not signed in.`; 404 workspace/issue messages as `GET` issue; 403 `{"error":"You don't have permission to do that."}` — reuse the existing messages/helpers).

Comment JSON: `{"id","author": person|null,"body": doc,"created_at","edited_at": string|null,"can_edit": bool,"can_delete": bool}`
Event JSON: `{"id","actor": person|null,"kind","data": {...},"created_at"}`

| Method & path | Needs | Fields | Success | Failures |
|---|---|---|---|---|
| `GET /api/workspaces/{slug}/issues/{identifier}/timeline` | member | query `before` (RFC 3339, optional) | `200 {"comments":[comment…],"events":[event…],"has_more":bool}`: at most 100 items in total, the newest created before `before` (exclusive; none = newest overall), each list oldest first (the client merges by `created_at`). The next page is `before=` the oldest `created_at` shown; a page never splits a shared `created_at` | 404; 400 `{"error":"before must be an RFC 3339 time."}` |
| `POST /api/workspaces/{slug}/issues/{identifier}/comments` | `comments.create` | `body` (doc JSON string) | `201 comment` | 400 `{"errors":{"body": msg}}` (`"Write a comment."` when empty, or the description doc messages with "Comment" instead of "Description", max 20,000 chars); 403 |
| `POST /api/workspaces/{slug}/comments/{id}` | edit rule | `body` | `200 comment` (sets `edited_at`) | 400; 403; 404 `{"error":"Comment not found."}` |
| `DELETE /api/workspaces/{slug}/comments/{id}` | delete rule | — | `204` | 403; 404 |

Mention emails for comments: newly mentioned members (new comment: everyone mentioned; edit: compared with the previous body), not the author. Subject `{Actor} mentioned you in {ENG-12}`, body `{Actor} mentioned you in a comment on {ENG-12}: {title}\n\n{link}\n` (reuse `notify.go`).

## Backend

- Comments live in a new package `internal/comment` (routes/controller/model) or inside `internal/issue` if it needs too much of the issue package's private helpers (access, allow, checkDoc, notify) — backend agent's call; keep one source of truth for the doc check and mention emails.
- Events: `internal/issue` (model writes them; `timeline` handler).
- Runnable check: a table test for the event diff (old vs new fields → events), and the `checkDoc` reuse for comments.
- Verify with curl: comment CRUD + each permission rule (author vs other member vs `comments.manage`, role without `comments.create`, archived project), every event kind appears once per real change (no event when the same value is sent again), description events coalesce, mention email in Mailpit.

## Frontend (user app)

- `api.ts`: `Comment`, `IssueEvent`, `Timeline` types.
- Issue page, under Relations: **Activity** section (`components/issues/timeline.tsx`). Loader fetches the timeline with the issue (`Promise.all`). Merged list oldest first:
  - Event row: small actor avatar (20px) or status icon for status events, one line of `text-caption text-secondary` "**Maya** moved from Todo to In progress · 2h ago" (actor name in `--text`), relative time with the full date in `title`.
  - Comment: card (nested surface, 8px radius) with avatar, name, relative time, "(edited)", the body via read-only `RichEditor`, and a "⋯" `Popover` menu with Edit / Delete when allowed. Edit swaps in an editable `RichEditor` with Save/Cancel. Delete asks in a `Dialog` ("Delete comment?", "This can't be undone.").
  - Composer at the bottom when the user has `comments.create` and the project isn't archived: `RichEditor` (placeholder "Leave a comment… Type @ to mention someone", mentions = members), "Comment" button (disabled while empty or pending); ⌘/Ctrl+Enter submits. After posting, clear the editor and `router.invalidate()`.
  - Relative time helper in `@taskwell/shared` (`src/time/relative.ts` + `relative.check.ts`): "just now", "5m ago", "3h ago", "yesterday", "Oct 12", "Oct 12, 2025" for other years.
- Live description edits from others don't refresh the timeline (refresh on navigation/after own actions is enough). `ponytail:` note it.
- Settings → Roles: the two comment permissions now show as active (the role form already renders inactive ones differently — check and make sure they're togglable).

## Verification

- Backend as above; `npm run build`; the relative-time check.
- Integration (headless Chrome, also iPad size): change status/assignee/labels/due date/priority/title/parent/link → each appears once in Activity; post a comment with formatting and @Maya → shows, Maya gets email; edit (shows "edited"), delete; Maya (member) can comment but can't edit/delete demo's comment; a role without `comments.create` sees no composer; archived project → no composer.
