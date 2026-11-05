# Invite prompt plan

Scope: signing up never joins an invited workspace by itself. Every new account gets its own workspace. After that,
pending invites for the person's email are offered in a prompt: "Join" or "Not now". "Not now" leaves a notification
in the Taskwell bot (the inbox's Taskwell DM, i.e. notifications) that can still join later.

Out of scope:
- Email reminders about pending invites.
- Inviting by link without an email.
- Bulk accept.

## Behaviour

- **Sign-up and first Google sign-in** (`workspace.CreateFirst`):
  - Always creates the user's own workspace, named from the sign-up's workspace name or "Ada's workspace".
  - `joinInvited` is removed, so invites stay pending.
  - The guest-projects join moves to the accept paths only (it already lives in `accept`).
- **The emailed invite link (`/invite?token=`)** works as today. Signing up from it lands back on the page, which offers "Accept invite" and now also "Not now". "Not now" goes to the user's home, where the prompt shows again until dismissed.
- **Prompt:** inside any workspace page, if the user has pending invites they haven't dismissed, a dialog shows the oldest one:
  - Title "Join <workspace>?".
  - Description "<inviter> invited you as <role>." The role name, plus "guest" wording when it's the Guest role.
  - Buttons "Not now" (ghost) and "Join workspace".
  - **Join** accepts and navigates to the new workspace.
  - **Not now** dismisses it, which adds the bot notification. The next pending invite (if any) shows after that.
  - **Closing** the dialog (Esc, X) counts as "Not now".
- **Bot notification** (kind `invite`): "<inviter> invited you to join <workspace>", with a "Join" button.
  - Joining accepts and navigates to the workspace.
  - If the invite is gone (accepted elsewhere, cancelled, expired), the row says "This invite is no longer available." and has no button.
  - Accepting an invite by any path deletes that invite's `invite` notifications.

## Schema (`backend/user/internal/db/migrations/021_invite_prompt.sql`)

```sql
alter table workspace_invites add column dismissed_at timestamptz;  -- the invited person said "Not now"
alter table notifications add column invite_id uuid references workspace_invites(id) on delete set null;
-- kind 'invite' added to notifications_kind_check; the target check allows invite notifications to have none of
-- issue/chat/project (num_nonnulls(issue_id, chat_id, project_id) = case when kind = 'invite' then 0 else 1 end).
```

`invite` notifications are never emailed. The instant and digest email code skips the kind.

## API

All of these need a signed-in user, and they act on invites for that user's email.

| Method & path | Success | Failures |
|---|---|---|
| `GET /api/me/invites` | `200 [{"id", "workspace": {"name"}, "invited_by": "<name or ''>", "role": {"id", "name"}, "guest": bool, "expires_at"}]`, plus `"dismissed": bool` ("Not now" was chosen; the prompt skips these, the workspace switcher lists them): unexpired, for a workspace the user isn't in, oldest first | 401 |
| `POST /api/me/invites/{id}/accept` | `200 Workspace` (same JSON as the token accept). Joins with the invite's role, a guest also to its projects; deletes the invite and its `invite` notifications | 401; 404 `"This invite link is invalid or has expired."` (same message the token path uses) when it isn't the user's, or it's unknown or expired |
| `POST /api/me/invites/{id}/dismiss` | `204`. FormData `workspace` is the slug the user is in now. Sets `dismissed_at` and inserts one `invite` notification for the user in that workspace (actor = inviter, `invite_id` = the invite), then publishes the notifications change. Dismissing again does nothing | 401; 404 as above; 404 `"Workspace not found."` when they aren't a member of `workspace` |

Notification JSON (the existing list) gets, for kind `invite`:
- `issue`, `chat` and `project` null
- a new field `"invite": {"id", "workspace": "<name>", "available": bool}`, where `available` is true when the invite still exists, is unexpired and the user hasn't joined
- `actor` is the inviter

Realtime: accept publishes the existing invite and member events, plus the user's notifications change. Dismiss publishes the notifications change. Add the path `/me/invites` (e.g. `realtime.MyInvites()`), published to the user on dismiss and accept, and to the invited person when an invite is created, renewed or cancelled (look the user up by email).

## Backend

- `workspace` package:
  - `routes.go`: the three routes.
  - `invite.go`: handlers.
  - `model.go`: `myInvites`, `acceptByID` (share the body with `accept`), `dismiss`.
  - Remove `joinInvited` and update `TestCreateFirstJoinsInvites`: a new invited user gets their own workspace, isn't in the invited one, and the invite is still pending and listed by `GET /me/invites`.
- `notify`:
  - The `invite` kind in the list query (join `workspace_invites` and `workspaces` for name and availability).
  - Skip it in emails.
  - A helper the workspace package calls to insert the notification in the same transaction.
- Tests:
  - List, accept and dismiss rules (someone else's invite gives 404, expired gives 404, accepted gives 404, dismiss twice gives one notification).
  - The notification's `available` flips after accept or cancel.
  - Accept deletes the notification.

## Frontend (user app)

- **`api.ts`:** `type PendingInvite`, the notification union gets `{ kind: 'invite'; invite: { id; workspace; available } }`, plus `acceptMyInvite(id)`, `dismissMyInvite(id, slug)` and `myInvitesPath`.
- **Prompt:** `components/invite/invite-prompt.tsx`, mounted once in the `/_app/$workspace` layout.
  - It fetches `GET /me/invites` (subscribed to the `/me/invites` path so it updates live) and shows the dialog described above.
  - Guests of a workspace see it too.
- **Bot row:** `components/inbox/feed-row.tsx` renders `invite` rows, using the text above, a "Join" button, or the unavailable line.
- **Invite page:** `routes/invite.tsx` gets a "Not now" link to home next to "Accept invite" (signed in, right email).
- **Signup form:** no change in behaviour. The invited sign-up still hides the workspace name field, and its account gets the default name.

Sentence case everywhere.

## Verification

- `go vet`, and `go test` for the `workspace`, `notify` and `auth` packages.
- `npm run build`.
- Integration: invite a new email, then sign up from the link. You land on your own workspace page with the prompt (or on the invite page). Pick "Not now" and the bot shows the invite; join from the bot and you're in. Cancel an invite and the bot row says it's no longer available.
