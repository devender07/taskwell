# Notes plan

Scope: Keep-style notes on their own page. A note has an optional title, a rich-text body that can hold checklists, a color, and per-person pin, archive and labels. The owner and anyone they share it with see and edit the same note. Shared notes show a "from <owner>" chip to everyone except the owner. Changes reach every open tab live through the existing realtime refetch.

The private "Notes" chat goes away. Its messages move into notes, and the chat stops creating `self` chats.

Out of scope:
- Live co-editing of the body (Yjs). `ponytail:` last write wins on title/body/color. If two people edit at once, one change is lost. Upgrade path: the issue description Yjs session.
- Email or in-app notification when a note is shared with you. It just appears in your notes.
- Reminders, images-as-cover, drawing, note paging. `ponytail:` GET returns every note the caller has. Page it if anyone has more than a few hundred.
- Guests: notes are for full members only.

## Schema (`backend/user/internal/db/migrations/019_notes.sql`)

```sql
create table notes (
  id           uuid primary key default uuidv7(),
  workspace_id uuid not null references workspaces(id) on delete cascade,
  owner_id     uuid not null references users(id) on delete cascade,
  title        text not null default '',
  body         jsonb,                       -- Tiptap doc; null when empty
  color        text not null default '' check (color = '' or color ~ '^#[0-9a-f]{6}$'),
  created_at   timestamptz not null default now(),
  updated_at   timestamptz not null default now(),
  updated_by   uuid references users(id) on delete set null
);
create index notes_workspace on notes (workspace_id);

-- Everyone who has the note, the owner included. Pin, archive and labels are each person's own.
create table note_members (
  note_id  uuid not null references notes(id) on delete cascade,
  user_id  uuid not null references users(id) on delete cascade,
  pinned   boolean not null default false,
  archived boolean not null default false,
  labels   text[] not null default '{}',
  added_at timestamptz not null default now(),
  primary key (note_id, user_id)
);
create index note_members_user on note_members (user_id);
```

The same migration moves the old notes. For every `self` chat, it copies each top-level message with `deleted_at is null` into `notes`, plus an owner `note_members` row. The copied note keeps the message's body and `created_at`, has `owner_id` = the chat's `created_by`, and an empty title. Then it deletes the `self` chats (their messages cascade or get deleted first, whichever the FKs need). `chat.model.list` stops inserting `self` chats, and `chat_test.go` is updated to match.

When someone leaves a workspace (`workspace.removeMember`, next to the `saved_views` delete), delete the notes they own in it and their `note_members` rows on other notes there.

## Rich text: checklists

- Backend `doc.Check` allows `taskList` and `taskItem`:
  - A `taskList` contains only `taskItem` nodes.
  - A `taskItem` has `attrs.checked` (bool) and the same children as `listItem`.
  - Add `taskItem` to `imageParents`.
  - `doc.PlainText` covers the new nodes.
- Frontend:
  - Add `@tiptap/extension-list@3.32.2` to `@taskwell/shared` dependencies. It's already installed as a starter-kit dependency, so this only makes the dependency explicit.
  - `RichEditor` gets a `tasks?: boolean` prop. When set, it adds TaskList/TaskItem (nested) and a "Checklist" toolbar button. Other editors don't change.
  - `RichText` renders `taskList` / `taskItem` as checkboxes.
  - `RichText` gets an optional `onToggleTask(doc)`. When given, the checkboxes are clickable and it returns the doc with that item's `checked` flipped. Without it they're read-only.

## API contract

All routes need a signed-in, non-guest member of the workspace. Guests get `403 {"error": "Notes aren't available to guests."}`. A note the caller isn't a member of returns `404 {"error": "Note not found."}`.

A note looks like this:

```json
{
  "id": "…", "title": "…", "body": {doc} | null, "color": "" | "#rrggbb",
  "owner": {"id", "name"},
  "members": [{"id", "name"}],      // everyone but the owner, by name
  "pinned": false, "archived": false, "labels": ["…"],   // the caller's own
  "can_delete": true,               // caller is the owner
  "created_at": "…", "updated_at": "…", "updated_by": {"id", "name"} | null
}
```

| Method & path | Who | Success | Failures |
|---|---|---|---|
| `GET /api/workspaces/{slug}/notes` | member | `200 [note…]`: all the caller's notes, archived too. Pinned first, then `updated_at` desc, then id | 401, 403, 404 |
| `POST /api/workspaces/{slug}/notes` | member | `201 note` (caller is owner and first member) | 400, 401, 403, 404 |
| `POST /api/workspaces/{slug}/notes/{id}` | note member | `200 note`; only fields present change | 400, 401, 403, 404 |
| `DELETE /api/workspaces/{slug}/notes/{id}` | note member | `204`. The owner deletes the note for everyone; anyone else leaves it | 401, 403, 404 |
| `POST /api/workspaces/{slug}/notes/{id}/members` | note member | `200 note` | 400, 401, 403, 404 |
| `DELETE /api/workspaces/{slug}/notes/{id}/members/{userId}` | owner (anyone), member (self only) | `204` | 400, 401, 403, 404 |

The create and update endpoints take these FormData fields:
- `title`: trimmed, at most 200 characters. Error: "Use 200 characters or fewer."
- `body`: Tiptap doc JSON, checked with `doc.Check(raw, "Note", 20000)`. An empty string means no body.
- `color`: `""` or `#rrggbb` (lowercased). Error: "Choose a color from the list."
- These are per caller:
  - `pinned`: `1` or `0`.
  - `archived`: `1` or `0`. Archiving also unpins.
  - `labels`: a JSON array of strings, each trimmed, 1 to 30 characters, case-insensitively unique, at most 20. Error: "Use up to 20 labels of 30 characters or fewer."
- On create, it's an error if both the title and the body are empty: `400 {"errors": {"body": "Write something first."}}`.
- Shared field changes (`title`, `body`, `color`) set `updated_at` and `updated_by`. Per-caller fields don't.

The add-member endpoint takes the FormData field `user_id`. It must be a non-guest member of the workspace who isn't already on the note, otherwise the error is "Choose someone in this workspace." There are at most 50 members per note. Past that the error is "A note can be shared with up to 50 people."

Removing a member: removing the owner returns `400 "The owner can't be removed. Delete the note instead."` A non-owner removing someone else gets 403.

Realtime: every change publishes the path `/workspaces/{slug}/notes` (add `realtime.Notes(slug)`) to `realtime.ToUser` for each member before and after the change. Removed members also get it, so the note disappears from their page.

## Backend

New package `backend/user/internal/note/` with `routes.go`, `controller.go`, `model.go`, `validate.go` and `note_test.go`, wired in `cmd/user/main.go` like `view`. Tests cover:
- visibility (non-member gets 404, guest gets 403)
- per-member pin/archive/labels not leaking
- owner delete vs member leave
- member add/remove rules
- validation
- the checklist `doc.Check` cases
- the migration's move of `self` chat messages (one test seeding a self chat, or SQL run in the test DB)

## Frontend (user app)

- `api.ts`: `type Note`.
- Route `src/routes/_app/$workspace/_main/notes.tsx`, at `/$workspace/notes`:
  - The loader calls `loaderApi` on `…/notes`.
  - `validateSearch` handles `label?: string`, `archived?: true` and `q?: string`. The filters are applied on the client.
- Sidebar: a "Notes" link after Inbox, hidden for guests, with a new `NoteIcon` in `icons.tsx`.
- Components in `src/components/notes/`:
  - **Top bar**: a search input (`q`) and filter chips: "Notes", one chip per label from the caller's notes, and "Archive".
  - **"Take a note…" composer**:
    - Collapsed, it's an input-like bar with a checklist button that starts with an empty checklist.
    - Expanded, it shows a Title input, a `RichEditor tasks`, a color swatch row (the `ColorPicker` SWATCHES set, plus "Default"), and a "Close" button.
    - Close saves when there's content and discards an empty note.
  - **Grid**: "Pinned" and "Others" sections, as a masonry layout using CSS `columns`. It's one column under 640px.
  - **Card**:
    - Shows the color as a subtle tint (`color-mix` with the surface) and the title.
    - The body is `RichText` with `onToggleTask` (it posts `body`), clamped in height.
    - Labels show as chips.
    - Shared notes show a **"From <owner first name>"** chip when the owner isn't you, and member avatars.
    - Hover/focus tools: Pin, Color, Labels, Share, Archive, More (Delete note for the owner, "Remove from my notes" for others).
    - Clicking the card opens a `Dialog` editor (title, `RichEditor tasks`, the same tools, last edited by / when). It saves on close and when tools are used.
  - **Share dialog**: the owner, then the members with a remove button. Members can remove themselves; the owner can remove anyone. There's a member search to add people (reuse `searchMembers` from `components/workspace/member-pages`).
  - **Labels popover**: checkboxes for existing labels plus a "Create label" input.
- Remove the old notes chat UI:
  - the `self` branches in `components/chat/*`
  - the `ShareNote` dialog and the "Share note…" menu item in `chat-view.tsx`
  - `self` from `ChatKind`
- Command palette: add a "Notes" go-to entry next to Inbox if the palette lists sections.

Sentence case everywhere. 44px touch targets on card tools.

## Verification

- Backend: `go vet`, `go test ./...` for `note`, `doc` and `chat`, plus curl against the running API: create, share, the other user sees it, toggle a checklist, leave, delete.
- Frontend: `npm run build`.
- Integration in the main session: two users. Create a checklist note, share it, and check the second user sees it live with the "From …" chip. Tick an item and see it on both. Pin, archive and label it on one side and check the other side is unaffected. Leave, then delete. Old Notes chat messages appear as notes after the migration.
