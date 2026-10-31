# Rich text, mentions and live editing plan

Scope (start of Phase 4): issue descriptions become rich text edited with Tiptap (MIT; built on ProseMirror), with `@mentions` of workspace members that notify by email, and several people can edit one description at the same time (Yjs, relayed and stored by the Go API). The editor is a shared component so Phase 4 comments reuse it.

Approved dependencies (no others):
- Frontend (`frontend/shared`): `@tiptap/react`, `@tiptap/pm`, `@tiptap/starter-kit` (includes link, underline, list keymap), `@tiptap/extensions` (placeholder), `@tiptap/extension-mention`, `@tiptap/suggestion`, `@tiptap/extension-collaboration`, `@tiptap/extension-collaboration-caret`, `@tiptap/y-tiptap`, `yjs`, `y-protocols`. All `3.x` / latest, all MIT (check `license` in each package.json after install).
- Backend: `github.com/coder/websocket`.

Out of scope: comments (next plan), image/file attachments, slash commands, tables, task lists, version history, rich text in the issue title, collaborative editing in the create dialog, a markdown export.

## Document format

A description is a **ProseMirror JSON document** (what `editor.getJSON()` returns), or nothing.

```json
{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Ping "},{"type":"mention","attrs":{"id":"<user uuid>","label":"Maya"}}]}]}
```

Allowed node types: `doc`, `paragraph`, `text`, `heading` (`attrs.level` 1–3), `bulletList`, `orderedList`, `listItem`, `blockquote`, `codeBlock`, `horizontalRule`, `hardBreak`, `mention`. Allowed marks: `bold`, `italic`, `strike`, `underline`, `code`, `link` (`attrs.href` must start with `http://`, `https://` or `mailto:`, any case). Anything else is rejected (`"Description has unsupported content."`). Max depth 32. Text length = every `text` node's text + every mention's `label`, counted in UTF-16 units like today: max 50,000 (`"Description must be 50,000 characters or fewer."`). A doc whose text length is 0 and has no mentions/horizontal rules is stored as null ("empty").

## Schema

Migration `backend/user/internal/db/migrations/012_rich_text.sql`:

```sql
-- descriptions become ProseMirror JSON; existing plain text becomes one paragraph per line
alter table issues add column description_doc jsonb;
update issues set description_doc = jsonb_build_object('type', 'doc', 'content', (
  select jsonb_agg(case when line = '' then jsonb_build_object('type', 'paragraph')
    else jsonb_build_object('type', 'paragraph', 'content', jsonb_build_array(jsonb_build_object('type', 'text', 'text', line))) end order by n)
  from regexp_split_to_table(description, E'\n') with ordinality as t(line, n)))
where description <> '';
alter table issues drop column description;
alter table issues rename column description_doc to description;

-- Yjs updates for a description being edited live; the client folds them into one row now and then
create table issue_doc_updates (
  id       bigint generated always as identity primary key,
  issue_id uuid not null references issues on delete cascade,
  data     bytea not null
);
create index on issue_doc_updates (issue_id, id);
```

## API contract

### Issue JSON and the existing endpoints

- `description` in every Issue response is the document object, or `null`.
- `POST …/projects/{key}/issues` and `POST …/issues/{identifier}`: FormData `description` is the document as a JSON **string**; `""` clears it. Errors: 400 `{"errors":{"description": msg}}` with the messages above, plus `"Description is not valid."` for bad JSON / root not `doc`.
- A `POST …/issues/{identifier}` that includes `description` also deletes the issue's `issue_doc_updates` rows (the live document is rebuilt from the saved JSON on the next connect). `ponytail:` people connected at that moment keep their old copy; fine because the issue page never saves descriptions through this endpoint.

### Mention emails

Whenever a description is saved (create, update, or a live `save` message below), the server compares mentioned user ids in the new document with the previous one. Each **newly** mentioned id that is a member of the workspace and isn't the person saving gets one email (sent in a goroutine with a 30s timeout so the request doesn't wait; failures logged; `mail.ErrNotConfigured` ignored):

- Subject: `{Actor name} mentioned you in {ENG-12}`
- Body: `{Actor name} mentioned you in {ENG-12}: {title}\n\n{app_base_url}/{workspace slug}/issue/{ENG-12}\n`

### Live editing (over the realtime socket)

Live editing runs over the tab's one realtime socket, `/api/realtime` (see `realtime-plan.md`): the client sends `{"type":"collab.join","workspace":"acme","issue":"ENG-12"}`, the server answers `collab.joined` with the issue's uuid, and `collab.leave` ends it. Editing allowed = the same `can_edit` rule as the issue JSON, recomputed on `workspace.permissions`.

Live-editing frames are **binary**: the first byte is the message type, bytes 1-16 the issue's uuid, the rest the payload. Max frame 1 MiB.

| Type | Direction | Payload | Server does |
|---|---|---|---|
| `0` ready | server → client | 1 flags byte (bit 0 = can edit, bit 1 = seed), then 8-byte big-endian id of the last stored update (0 when none) | Sent once, after every stored update has been sent as type `1` frames, oldest first |
| `1` update | both | a Yjs update (`Y.encodeStateAsUpdate` / `doc.on('update')`) | From a client that can edit: insert into `issue_doc_updates`, then forward to every other connection on the issue. From a read-only client: ignored |
| `2` awareness | both | a y-protocols awareness update | Forward to every other connection on the issue (any member); not stored |
| `3` compact | client → server | 8-byte big-endian id (the `ready` id the client loaded up to), then a Yjs update with the client's whole state | Can edit only. In one transaction: delete the updates this connection loaded with `id <= that id`, insert the payload. Not forwarded |
| `4` save | client → server | the document as UTF-8 JSON (`editor.getJSON()`) | Can edit only. Validate like the description field (invalid: log + ignore); store as `issues.description` (`updated_at = now()`; null when empty) without clearing updates; send mention emails |
| `5` seed | server → client | none | Sent to another editor when the seeder disconnects before its seed update was stored; the client seeds as if `ready` had the seed bit |
| `6` reset | server → client | none | Sent to every connection on the issue, on every instance, when a REST write (`POST …/issues/{identifier}` with `description`) replaced the description: the stored updates and the seed claim are cleared, and the server ignores that connection's frames from then on. The client stops sending, reloads the issue, drops its `Y.Doc` and joins again, so the next editor seeds from the new description instead of pushing the old document back |

**Seed:** when an issue has no stored updates but has a saved description, the first connection to that issue gets the seed bit; it builds the Yjs document from the saved JSON and sends it as a normal update. The room remembers it handed out the seed, so later connections don't (they receive the seed update when it arrives). If the seeder disconnects before its seed update is stored, the seed passes to another editor in the room (type `5`), or to the next editor to join.

Rooms: in memory, one per issue id, in a small hub in the issue package (mutex + map; a connection whose send buffer is full is disconnected). Other instances get each session's frames through Postgres `LISTEN/NOTIFY` on the `collab` channel (`issue/listen.go`): small updates and awareness inline, bigger updates by stored id. Awareness too big for a notification is dropped on every instance.

## Backend (`backend/user/internal/issue/`)

- `doc.go`: the document type + `checkDoc(raw) (normalized []byte|nil, errMsg)` (allowed types, href schemes, depth, length, empty → nil) and `mentions(doc) []string`. **Runnable check**: `doc_test.go` table test (valid doc, empty doc, unknown node, `javascript:` link, too long, mentions listed once).
- `collab.go`: live-editing sessions (joined over the realtime socket) + hub; `listen.go` relays other instances' frames. Store queries stay in `model.go`.
- `notify.go`: mention emails. `issue.New` gains the mailer and `sysconfig.Store` (`app_base_url`), wired in `cmd/user/main.go`.
- Verify: `go vet`, `go test`, curl the form endpoints (valid doc, bad JSON, bad node, `javascript:` link, `""` clears), migration converts the seeded demo descriptions, and a tiny Go or Node WebSocket check: two clients, one sends an update, the other receives it; reconnect receives it from storage; read-only client's update is ignored; mention email arrives in Mailpit (`localhost:8025`).

## Frontend

### Shared `RichEditor` (`frontend/shared/src/components/rich-editor/`, export `@tether/shared/rich-editor`)

```ts
type Doc = JSONContent // from @tiptap/react
type RichEditorProps = {
  label?: string
  'aria-label'?: string
  content?: Doc | null           // initial content when not collaborative (ignored after mount)
  onChange?: (doc: Doc | null, textLength: number) => void // null when empty
  editable?: boolean             // default true
  placeholder?: string
  mentions?: { id: string; name: string }[] // empty or absent: no @ suggestions
  error?: string | null          // uses useFieldError like Input
  collab?: { doc: Y.Doc; awareness: Awareness; user: { name: string; color: string } } // when set, content comes from the Y.Doc
  minHeight?: number
}
```

- StarterKit (heading levels 1–3; `link: { openOnClick: !editable, autolink: true }`), Placeholder, Mention (`suggestion.char: '@'`), and with `collab`: Collaboration (`document: doc`, StarterKit `undoRedo: false`) + CollaborationCaret.
- Looks like the `Input`/`Textarea` field (same border, radius 6px, focus ring) with a small toolbar above the text when editable: Heading, Bold, Italic, Strike, Code, Bullet list, Numbered list, Quote, Code block — `IconButton`-sized touch targets (44px), `aria-pressed` for active marks. Read-only: no border, no toolbar, just the formatted text.
- Mention suggestions: our own popup (no floating-ui), `position: fixed` at the suggestion's `clientRect()`, styled like the SmartAutocomplete list, filtered with SmartAutocomplete's `match.ts`, max 8 rows, ↑/↓/Enter/Tab/Esc, tap to pick. Empty: "No people found". Mentions render as `@Name` in link color.
- Content styles in `rich-editor.css` using DESIGN.md tokens only (text sizes, Berkeley Mono for code, quote border, lists, caret labels).

### Validators

`issueDescription` in `@tether/shared/validate` takes the text length instead of a string: `(length: number) => string | null`. Update its callers.

### User app

- `vite.config.ts`: `'/api': { target: 'http://localhost:8090', ws: true }`.
- `api.ts`: `Issue.description: JSONContent | null`; export `refreshAccess` for the collab client.
- `components/issues/collab.ts`: `connectCollab(workspace, issue, doc, awareness, onState, onPresence, onTooLarge, onReset)` — joins the issue over the shared realtime socket (`joinCollab` in `realtime.ts`), applies `1` frames, on `ready` reports `{ canEdit, seed, lastId, loaded: count }`; sends local doc updates (origin ≠ remote) as `1`, awareness changes as `2`; after `ready`, if it loaded more than 100 updates, sends `3` compact with its state. Re-joins when the socket reconnects. On `6` reset it stops sending and calls `onReset`; `description.tsx` reloads the issue and starts over with a fresh `Y.Doc` seeded from it. Returns `destroy()`. Read-only clients still connect (to watch live edits and presence).
- `components/issues/description.tsx`: the issue page description. Creates a `Y.Doc` + `Awareness`, connects, shows the saved JSON read-only until `ready`; then the collaborative `RichEditor` (editable when `can_edit` and the server agrees). On `seed`, fill the Yjs doc from the saved JSON (`prosemirrorJSONToYXmlFragment` via `@tiptap/y-tiptap`, or `editor.commands.setContent` once). After local edits, debounce 1s and send `4` save with `editor.getJSON()`; flush on unmount / `pagehide`. Caret user = current user's name + a color from `--accent`, `--success`, `--link` picked by user id hash. No save indicator; a muted caption "Offline, reconnecting…" shows while disconnected.
- Issue page: description section uses the component above (no "No description." text when editable — the placeholder "Add a description… Type @ to mention someone" shows instead; read-only and empty → "No description."). The **Edit** form becomes title only.
- Create issue dialog: `RichEditor` (not collaborative) with mentions = workspace members; submits `JSON.stringify(doc)` or `""`.
- Anywhere else descriptions are shown? (Project list/board don't show them.) Nothing else changes.

## Verification

- Backend: above.
- Frontend: `npm run build`.
- Integration (main session, headless Chrome, also iPad size): create an issue with bold text, a list and `@Maya` → saved and rendered; Maya gets an email in Mailpit; demo descriptions were converted; open the same issue in two browser contexts (demo + maya) → typing in one appears in the other with a named caret; reload → content persists; read-only member sees live edits but can't type; going offline shows the notice and reconnects.
