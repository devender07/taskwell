# Realtime plan

Scope: the whole product updates live, the user app and the admin app. Every signed-in tab keeps **one** WebSocket open. It carries three things:

1. **Events.** When anyone changes something, the server sends every affected tab a small event naming the REST paths that changed. The tab refetches exactly those paths through the normal REST API. Data never travels in events, only the "go and reload this" hint, so REST stays the single source of data and of permission checks.
2. **Live description editing.** The Yjs protocol from `docs/rich-text-plan.md` moves onto this socket as binary frames tagged with the issue. The separate `…/issues/{identifier}/collab` socket is removed.
3. **Presence.** "Who's viewing this issue": everyone with the issue open sees the avatars of the others, across API instances.

Out of scope: typing indicators in comments, offline queueing of writes, presence on lists and the board (issue page only).

## How it works

```
 tab A ── POST /api/…/issues/ENG-12 ─▶ API ─ tx: update + pg_notify('realtime', event) ─▶ Postgres
                                                                                            │ (on commit)
 every API instance ◀──────────────────── LISTEN realtime ◀─────────────────────────────────┘
        │ fan out to local sockets whose user is in the event's audience
        ▼
 tab B ◀─ {"paths":["/workspaces/acme/issues/ENG-12", …]} ─ refetch those paths via REST
```

- **Published inside the write's transaction** with `pg_notify`. Postgres only delivers a notification when the transaction commits, so a tab is never told to refetch something that later rolled back, and never refetches before the data is visible.
- **Every instance listens** (the same pattern as `issue/listen.go`, its own channel `realtime`), including the instance that made the change. So one code path delivers to everyone, on one server or several.
- **The hint only carries paths and ids**, never titles or bodies, and REST re-checks permissions on the refetch. A wrongly delivered hint can therefore leak at most that an id changed.

## Socket: `GET /api/realtime` (WebSocket)

- Auth is the `tether_access` cookie on the upgrade request. 401 before upgrading when signed out. Origin is checked by `websocket.Accept` defaults.
- One per tab, opened by the `_app` layout. Text frames, JSON.
- **Client → server:**
  - `{"type":"subscribe","workspace":"acme"}` is sent on connect and whenever the current workspace changes. The server checks membership (404-style `{"type":"error","error":"Workspace not found."}` frame, no close) and replaces the previous workspace subscription.
  - User-scoped events (your notifications, your profile) are always on; they need no subscription.
  - `{"type":"ping"}` → `{"type":"pong"}`. The client pings every 25s. The server also sends WebSocket pings every 30s, as `collab.go` does.
- **Server → client:**
  - `{"type":"hello","client":"<connection id>"}` is sent first.
  - Events, as below.
- **The tab's own changes:** every REST call from that tab carries the header `X-Tether-Client: <connection id>`. The API copies it into the events it publishes as `origin`, and the client skips events whose `origin` is its own id, since the tab already reloads after its own writes.
- **Membership changes:** when a user is removed from a workspace or loses access, their subscription to it is dropped and they get a `workspace.removed` event. When a member's role changes, they get `workspace.permissions` (see below). Permissions are not re-checked per event, because the hint carries no data.
- Limits: read limit 4 KiB per frame (clients only send small control frames); a send buffer of 64 events per connection, and a client whose buffer is full is disconnected.

### Event

```json
{"type":"event","kind":"issue.updated","workspace":"acme","actor":"<user id>","origin":"<connection id or empty>",
 "paths":["/workspaces/acme/issues/ENG-12","/workspaces/acme/projects/ENG/issues","/workspaces/acme/my-issues"],
 "at":"2026-10-31T10:00:00Z"}
```

- `paths` are REST paths **without** the `/api` prefix (the same strings the frontend passes to `api()`).
- The client matches a changed path against any path it has loaded, ignoring the query string. A path ending in `/` is a prefix: `"/workspaces/acme/"` means "everything in this workspace".
- **Audience** is server-side only, never sent: either `workspace` (every subscribed member) or `user` (all of one user's connections).
- **Event size:** an event must fit in a NOTIFY payload (< 8000 bytes). If the paths would exceed that, collapse them into the workspace prefix `"/workspaces/acme/"`.

## Live editing on the same socket

The frame types, seed rules, compaction and storage from `docs/rich-text-plan.md` (and the multi-instance relay from `issue/listen.go`) stay exactly as they are. Only the transport changes.

- **Join** (text): `{"type":"collab.join","workspace":"acme","issue":"ENG-12"}`.
  - The server runs the same checks the old upgrade did: signed in, member, issue exists, `can_edit`.
  - Answer: `{"type":"collab.joined","issue":"ENG-12","id":"<issue uuid>"}`, or `{"type":"collab.error","issue":"ENG-12","error":"Issue not found."}`.
  - After `collab.joined` it sends the stored updates and the `ready` frame, as today.
  - A tab can be joined to several issues at once; joining the same issue again restarts it (fresh replay + `ready`).
- **Leave** (text): `{"type":"collab.leave","issue":"<issue uuid>"}`. Closing the socket leaves everything.
- **Binary frames, both ways:** byte 0 = the existing message type (0 ready, 1 update, 2 awareness, 3 compact, 4 save, 5 seed), bytes 1–16 = the issue uuid as 16 raw bytes, then the existing payload. A binary frame for an issue the connection hasn't joined is ignored.
- **Size:** the socket's read limit is 1 MiB, as the collab socket's was. Text control frames over 4 KiB are rejected (closing the connection).
- **Removal:** the old `GET …/issues/{identifier}/collab` route and its handler entry point are removed. The hub, seed claim, compaction and LISTEN relay are reused unchanged, now keyed by (connection, issue) instead of one connection per issue.

## Presence

- **Who counts as viewing:** joining collab for an issue (the issue page) means "viewing". A read-only member is still joined, and so counted.
- **Storage:** `issue_viewers (conn_id text, issue_id uuid, user_id uuid, instance text, seen_at timestamptz, primary key (conn_id, issue_id))`, in migration `019_presence.sql`.
  - A row is inserted on join, deleted on leave or disconnect, and `seen_at` is refreshed every 30s by the owning instance for all of its rows.
  - A row older than 90s counts as gone, which covers a crashed instance. Each instance deletes stale rows once a minute.
- **Changes:** on every change to an issue's viewers, the instance publishes `pg_notify('realtime', …)` with audience `issue:<id>`. Every instance then sends its local joined connections:

  ```json
  {"type":"presence","issue":"ENG-12","viewers":[{"id","name"}]}
  ```

  - `viewers` is the de-duplicated list of users, by user id, whose rows are fresh. It includes the receiver, and the client hides itself from the list.
  - A user with the issue open in two tabs appears once.
  - `presence` is also sent right after `collab.joined`.
- **Frontend:** a row of up to 5 small avatars (24px, "+N" after that) with names in `title`, at the right of the issue page header next to Watch. Its `aria-label` is "Also viewing: Maya Member".

## Admin app

- **The admin service** (`backend/admin`) gets its own `GET /api/realtime` socket. Auth is the `tether_admin` cookie; it checks `platform_admin` and not suspended at upgrade time. It carries text events only: no subscribe, no collab, no presence.
- **Listening:** the admin service LISTENs on the same `realtime` channel and handles only audience `admin`. It sends those events to every admin connection.
- **Publishing:** both services publish `admin`-audience events in the same transactions as the changes:

| Change | Published by | kind | paths |
|---|---|---|---|
| Sign-up (any method) | user | `admin.users` | `/users`, `/stats` |
| Workspace create / rename / delete; member added or removed | user | `admin.workspaces` | `/workspaces`, `/stats` |
| Project / issue create or delete; comment create or delete | user | `admin.stats` | `/stats`, `/workspaces` (counts) |
| Suspend / unsuspend | admin | `admin.users` | `/users` |
| Config change | admin | `admin.config` | `/config` |

- **Paths** are the admin API's paths without `/api`.
- **Origin:** the admin app sends `X-Tether-Client` like the user app, and the admin service honours `origin` the same way.
- **Frontend:** `frontend/admin/src/realtime.ts` works like the user app's: it records the paths each loader loaded, invalidates matches on events, and reconnects with backoff. A 401 on connect sends the tab to `/signin`.
  - `/stats` events are throttled on the client to at most one refetch per 5s.
  - A config row being edited keeps its typed value.

## Event catalog

Every write endpoint publishes, in the same transaction, the events below. `{ws}` = `/workspaces/{slug}`, `{i}` = the issue identifier, `{k}` = the project key.

| Change (endpoint) | kind | audience | paths |
|---|---|---|---|
| Create issue (`POST {ws}/projects/{k}/issues`) | `issue.created` | workspace | `{ws}/projects/{k}/issues`, `{ws}/projects/{k}`, `{ws}/my-issues`, plus the parent's `{ws}/issues/{parent}` when it's a sub-issue |
| Update issue, incl. board drag (`POST {ws}/issues/{i}`) | `issue.updated` | workspace | `{ws}/issues/{i}`, `{ws}/issues/{i}/timeline`, `{ws}/projects/{k}/issues`, `{ws}/my-issues`, the old and new parent's `{ws}/issues/…`, and the `{ws}/issues/…` of every issue linked to it (their refs show its status/assignee) |
| Live description `save` (collab socket) | `issue.updated` | workspace | `{ws}/issues/{i}/timeline`, `{ws}/projects/{k}/issues` (the description itself is already live over collab) |
| Delete issue | `issue.deleted` | workspace | `{ws}/issues/{i}`, `{ws}/projects/{k}/issues`, `{ws}/my-issues`, the parent's and linked issues' `{ws}/issues/…` |
| Link / unlink | `issue.updated` | workspace | both issues' `{ws}/issues/…` and `…/timeline` |
| Watch / unwatch | `issue.watch` | user (the caller) | `{ws}/issues/{i}` |
| Comment create / edit / delete | `comment.changed` | workspace | `{ws}/issues/{i}/timeline` |
| Project create / update / archive / unarchive (`{ws}/projects…`) | `project.changed` | workspace | `{ws}/projects`, `{ws}/projects/{k}` (old and new key when it changed), `{ws}/my-issues` |
| Status create / update / order / delete | `workflow.changed` | workspace | `{ws}/statuses`, and for delete-with-move `"{ws}/"` (issues changed status) |
| Label create / update / delete | `workflow.changed` | workspace | `{ws}/labels`, and on update/delete `"{ws}/"` (issue rows show label names) |
| Workspace rename / slug change (`POST {ws}`) | `workspace.changed` | workspace | `{ws}`, `/workspaces`; with a slug change the event also carries `"new_slug"` so tabs on the old URL navigate to the new one |
| Workspace delete | `workspace.removed` | workspace | `/workspaces` (tabs inside it navigate to `/`) |
| Invite create / resend / cancel | `members.changed` | workspace | `{ws}/invites` |
| Invite accepted / member added | `members.changed` | workspace | `{ws}/members`, `{ws}/invites` |
| Member role change (`POST {ws}/members/{id}`) | `members.changed` (workspace) + `workspace.permissions` (user: that member) | as listed | `{ws}/members`; the user event's paths: `{ws}`, `"{ws}/"` (their permissions changed, so everything they see may have changed) |
| Member removed / left | `members.changed` (workspace) + `workspace.removed` (user: that member) | as listed | `{ws}/members`; the user event's paths: `/workspaces` |
| Role create / update / delete | `roles.changed` | workspace | `{ws}/roles`, `{ws}/members`, and for an update/delete also `workspace.permissions` to every member holding that role |
| New notification / read / unread / read all | `notifications.changed` | user (the recipient / the caller) | `{ws}/notifications`, `{ws}/notifications/unread-count` |
| Profile name / avatar (`POST /me`, `/me/avatar`) | `profile.changed` | user (own tabs) + `members.changed` to every workspace they're in | `/me`; for workspaces `{ws}/members` and `"{ws}/"` (names and avatars show on issues and comments) |
| Notification settings (`POST /me/notification-settings`) | `profile.changed` | user | `/me/notification-settings` |

Not published: auth endpoints (sign-in, refresh and the like), `POST {ws}/files` (an upload changes nothing until a doc references it), `POST /workspaces` (creating a workspace only matters to the creator's own tab, which already reloads).

The **email worker** marking rows as emailed publishes nothing. Inserting notifications does publish, because `notify.Record` runs inside the change's transaction, so the recipients' bells update instantly instead of after the 60s poll.

## Backend

- **New package `internal/realtime`:**
  - `routes.go`: the socket route.
  - `hub.go`: connections indexed by user id and by workspace id.
  - `listen.go`: LISTEN on `realtime` with reconnect. After a reconnect it sends every local connection `{"type":"resync"}`, because events may have been missed.
  - `publish.go`: `Publish(ctx, tx pgx.Tx, e Event) error`, which calls `pg_notify` inside the caller's transaction.
  - `event.go`: the `Event` type and the path helpers (`IssuePaths(ws, identifier, key)` and so on), so each feature package builds paths the same way.
- **Origin:** a tiny middleware or helper reads `X-Tether-Client` (a max-64-char token; anything else is ignored) into the request context, and `Publish` copies it into `origin`.
- **Every write in the catalog** calls `Publish` inside its existing transaction. Writes that today are single statements without a transaction become a transaction (`pgx.BeginFunc`).
  - Feature packages depend on `realtime` only through `Publish` and the path helpers.
  - Wiring is manual, in `main.go`.
- **The NOTIFY payload** is the event JSON plus the audience (`{"aud":"ws:<workspace id>"|"user:<user id>", "event":{…}}`). The workspace id rather than the slug is used, so a slug change can't misroute.
- **Collab moves** from `internal/issue/collab.go`'s HTTP handler to `internal/realtime` dispatching join/leave/binary frames into the issue package's hub. The issue package exposes a small API, for example `Join(ctx, conn, userID, workspace, identifier) (issueID, error)`, `Leave` and `Frame`, so `realtime` doesn't import issue internals and issue doesn't know about sockets.
- **Presence** lives in `internal/realtime/presence.go`, with SQL in `realtime/model.go`.
- **Admin:** `backend/admin/internal/realtime` holds the admin socket, its listener and `Publish`. The user service's admin events are published with the user service's `realtime.Publish` using audience `admin`.
- **Runnable checks:**
  - a table test of the path helpers and the payload size cap (collapse to the workspace prefix);
  - a hub test: a workspace event reaches only that workspace's subscribers, and a user event reaches only that user;
  - an origin test.
- **Verify with a WebSocket check program**, like `scratchpad/wscheck`:
  - Two users subscribed to acme, plus one user not in acme.
  - Each catalog row is triggered once with curl, and the expected events (and only those) arrive at the expected sockets.
  - A rolled-back write publishes nothing.
  - A run on two API instances (8090 and 8091) delivers across instances.
  - Killing the listener connection sends `resync`.
  - Collab over the new socket:
    - The old `wscheck` scenarios pass: relay, read-only ignored, seed and seed hand-off, compaction, cross-instance.
    - Two issues are joined on one socket without crosstalk.
    - A binary frame for an issue that wasn't joined is ignored.
  - Presence:
    - Join on 8090 and on 8091 → both see 2 viewers.
    - Kill a connection → viewers drop.
    - Stop an instance without cleanup → its viewers vanish after 90s.
  - Admin: a sign-up via curl → an admin socket receives `admin.users`.

## Frontend (user app)

- **`src/realtime.ts`:**
  - **Connection:** connects to `ws(s)://host/api/realtime` and stores the connection id from `hello`.
  - **Header:** `api()` sends `X-Tether-Client: <connection id>` once it's known.
  - **Subscription:** subscribes to the current workspace whenever the `$workspace` param changes.
  - **Reconnect:** reconnects with backoff from 1s to 10s. If the connection fails before `hello`, it calls `refreshAccess()` first, as `collab.ts` does. After any reconnect it treats the reconnect as a `resync`.
  - **Pings:** every 25s.
- **Knowing what's on screen:**
  - `api()` records every GET path it loads into a `Set` per route match, using a small wrapper `loaderApi(match, path)` or an equivalent that the frontend agent picks. That gives a map from route match to the paths it loaded.
  - Components that fetch outside loaders (unread count, inbox pages, quick-create data, the palette's search, the SmartAutocomplete pickers) register with `subscribePaths(paths, refetch)`, which returns an unsubscribe function.
- **Handling events:**
  - Events are collected for 150ms and their paths de-duplicated.
  - Then the client runs `router.invalidate({ filter: m => loadedPaths(m).some(changed) })` and calls every `subscribePaths` callback whose paths match.
  - `resync` (or a reconnect) invalidates everything.
  - Events whose `origin` is this tab are skipped.
- **Special kinds:**
  - `workspace.changed` with `new_slug`: if the tab is on the old slug, it `navigate`s to the same route with the new slug (`replace: true`).
  - `workspace.removed`: if the tab is in that workspace, it navigates to `/` with the toast "You no longer have access to Acme." (a muted caption on the home page, no toast library).
  - `workspace.permissions`: invalidate everything in that workspace; the routes' existing guards redirect if a page is no longer allowed.
- **Edits in progress are never clobbered.**
  - Open forms (Edit title, the comment being edited, role editor, settings forms) keep the user's typed values. Loaders refresh around them, as they already do on `router.invalidate()`, because form state lives in component state.
  - The comment composer and the editors don't reset on refetch.
  - If an open issue gets deleted, the issue route's loader 404s and the page shows the existing not-found screen.
- **Replacing polling:** the unread-count 60s poll stays as a fallback but slows to 5 minutes while the socket is connected (`ponytail:` remove it once the socket has proven itself).
- **Collab moves onto the shared socket:** `components/issues/collab.ts` uses `realtime.ts`'s `joinCollab(workspace, identifier)`. That returns a channel with `send(type, payload)`, an `onFrame` callback and `leave()`. Everything else in collab.ts stays the same: ready, seed, compact, offline edits, awareness.
  - When the shared socket reconnects, it re-joins every open collab channel, and each channel then behaves like a collab reconnect does today.
  - `description.tsx` gets presence from the same channel (`onPresence(viewers)`) and renders the viewers row in the issue header.
- **No new dependencies.** The `api()` helper and router are already in place.

## Verification (integration, headless Chrome, two browser contexts: demo and maya)

1. **Board and issue changes:**
   - Demo drags ENG-3 to Done on the board → Maya's open board and list move it within a second, and her ENG-3 page shows Done.
   - Demo changes title, priority, labels or due date → Maya's issue page and project list update.
2. **Comments:** Maya comments → Demo's timeline shows it. Demo's own tab doesn't double-refetch (check network: one timeline GET).
3. **Notifications:** Demo assigns Maya → Maya's bell goes to 1 immediately, with no 60s wait.
4. **Projects and workflow:** Demo renames the project, archives it, adds a label or reorders statuses → Maya's sidebar, lists and pickers update.
5. **Workspace and members:**
   - Demo changes the workspace slug → Maya's tab moves to the new URL.
   - Demo changes Maya's role to one without `issues.create` → Maya's New issue button disappears.
   - Demo removes Maya → Maya's tab goes home with the notice.
6. **Profile:** Maya uploads an avatar → Demo's members list and comment avatars update.
7. **Connection loss:** kill the API and restart it → both tabs reconnect and resync. Two API instances, Demo on 8090 and Maya via a second Vite proxy or a direct check program on 8091 → events still arrive.
8. **Live editing over the merged socket:** two tabs on ENG-1 still edit live with named carets, and no `/collab` request appears in the network log.
9. **Presence:** Maya opens ENG-1 → Demo's ENG-1 header shows Maya's avatar ("Also viewing: Maya Member"). Maya navigates away → it disappears within a second.
10. **Admin app:** with the admin Overview and Users open, sign up a new user in the user app → the Users list and the stat tiles update. Suspend a user in one admin tab → a second admin tab updates.
11. **iPad size:** the same flows work, and the socket survives the tab being backgrounded and foregrounded (reconnect on `visibilitychange`).
