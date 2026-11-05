# Notes: live collaboration and share permissions

Follow-up to `docs/notes-plan.md`. It does two things:
- Shared notes edit live, with Yjs and named carets, the way issue descriptions do.
- Each person a note is shared with either **can edit** or **can view**.

Out of scope:
- A live title. The title stays a plain field, autosaved over REST with last write winning. Only the body is live.
- "Who's viewing" presence for notes. Carets already show who's editing.
- Live editing while writing a new note. The new-note popup keeps REST autosave; a note is live once it's reopened.

## Schema (`backend/user/internal/db/migrations/020_note_collab.sql`)

```sql
alter table note_members add column role text not null default 'edit' check (role in ('edit', 'view'));
-- The live document's stored Yjs updates, like issue_doc_updates (same columns and compaction rules).
create table note_doc_updates ( ... mirror issue_doc_updates, note_id uuid not null references notes(id) on delete cascade ... );
```

The owner's row is always `edit`.

## API changes

- **Note JSON:**
  - `members` items become `{"id", "name", "role"}`, where `role` is `"edit"` or `"view"`.
  - New field `can_edit`: true for the owner or an `edit` member.
- **`POST /notes/{id}` (update):** a viewer sending `title`, `body` or `color` gets `403 {"error": "You can only view this note."}`. `pinned`, `archived` and `labels` stay allowed for everyone, since they're per person.
  - A REST `body` write while the note has a live session resets the session, the same way an issue's REST description write does.
- **`POST /notes/{id}/members` (add):** FormData fields `user_id` and `role`. `role` is `edit` (the default) or `view`; anything else returns `400 {"errors": {"role": "Choose view or edit."}}`. Only people who can edit may add; others get 403.
- **`POST /notes/{id}/members/{userId}` (new, change role):** FormData field `role`, returns `200 note`.
  - Only the owner may change roles; anyone else gets 403 `"You don't have permission to do that."`
  - Changing the owner returns `400 {"error": "The owner can always edit."}`
  - A user who isn't a member returns 404 `"Note not found."`
- **Removing a member:** the rules don't change.
- **Realtime:** a role change publishes `/workspaces/{slug}/notes` to every member.

## Live editing over the socket

This reuses the issue machinery: `realtime.Collab`, the frame types in `issue/collab.go`, compaction, the seed and reset.

- **Join:** `{"type": "collab.join", "workspace": "<slug>", "note": "<note id>"}`.
  - On success the reply is `{"type": "collab.joined", "note": "<note id>", "id": "<note id>"}`. Otherwise it's `{"type": "collab.error", "note": "<note id>", "error": "…"}`.
  - `collab.join` with `issue` keeps working unchanged.
- **Leave:** `{"type": "collab.leave", "issue": "<session uuid>"}` stays as it is (it's keyed by uuid), so it also works for notes.
- **Binary frames:** the same format, tagged with the note's uuid.
- **Who can join:** members of the note only. A non-member gets `collab.error` "Note not found." A `view` member joins read-only (`canEdit=false`), so they get updates but their update frames are ignored.
- **Save:** the save frame writes `notes.body` after `doc.Check(…, "Note", 20000)`, plus `updated_at` and `updated_by`. It publishes the notes path to every member, debounced like the issue saves. If the note has no stored updates, the session seeds from `notes.body`.
- **Layout:** the backend agent should pull the room, hub and storage code out of `issue/` into a shared package (e.g. `internal/collab`) used by both issues and notes, and not copy it. Issue behaviour and tests must stay the same.

## Frontend

- **Shared code:**
  - `realtime.ts` `joinCollab` gets the kind, `issue` or `note`, and sends the right field.
  - `components/issues/collab.ts` (the Y.Doc, awareness and seed hook) works for notes too. Move it if that's cleaner.
  - `seedYDoc` gets a `tasks` option, so the seed schema includes TaskList and TaskItem for notes.
- **Note popup (`NoteDialog`):**
  - The body uses `RichEditor collab tasks` with named carets, and the body REST autosave is removed. The title and color keep REST autosave.
  - "Last saved" follows the note's `updated_at` from realtime reloads.
- **Viewers (`!can_edit`):**
  - The title is read-only and the editor is `editable={false}`, but still updates live.
  - Only pin, archive, labels and "Remove from my notes" remain.
  - A **"View only"** chip shows in the popup header.
  - Checklist ticks on cards are disabled.
- **Share dialog:**
  - Each member has a role control ("Can edit" / "Can view") that only the owner can change; everyone else sees the role as text.
  - Adding people has a role choice that defaults to "Can edit". Only editors see the add field.
- **Cards:** a note shared with you read-only shows a "View only" chip next to "From <name>".

Sentence case everywhere.

## Verification

- Backend:
  - `go vet`; `go test` for the issue (collab behaviour unchanged), note and realtime packages.
  - Tests for the roles: viewers are 403 on title, body and color, and can still pin; only the owner changes roles; the owner can't be demoted; viewers join read-only.
  - A save frame persists the body.
- Frontend: `npm run build`.
