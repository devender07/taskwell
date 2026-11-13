# Team invites plan

Scope: an admin invites people to a workspace by email from **Settings → Members**, sees pending invites, and can resend or cancel them. The invitee opens the emailed link, signs in or signs up with that email, and joins the workspace as a **member**. Everyone in the workspace can see the member list; only admins see and manage invites.

Update (shipped follow-up):
- **Auto-join:** opening `/invite` while signed in with the invited email accepts it in the loader and lands in the workspace; no Join button (it only shows as a retry if accepting fails). Signed out, sign-in/sign-up redirect back to `/invite`, which then joins the same way.
- **Invite from Workspace settings:** admins also get the invite form on Settings → Workspace.

Out of scope (next task, "Roles"): changing a member's role, removing members, leaving a workspace, inviting as admin.

Known limits:
- Accepting needs the signed-in email to match the invite (a forwarded link can't be used by someone else).
- "Continue with Google" from the invite's sign-in page lands on `/`, not back on the invite; the user reopens the email link. `ponytail:` carry `redirect` through the Google state cookie if this bites.
- Signing up with an email that has a pending invite joins the invited workspaces instead of creating a default workspace (`joinInvited` in `workspace/default.go`). The invite stays so the emailed link still lands them in the workspace, but it's hidden from the pending list once they're a member.

## Schema

Migration `backend/user/internal/db/migrations/006_invites.sql`:

```sql
create table workspace_invites (
  id           uuid primary key default uuidv7(),
  workspace_id uuid not null references workspaces on delete cascade,
  email        text not null,
  token_hash   bytea not null unique,
  invited_by   uuid references users on delete set null,
  expires_at   timestamptz not null,
  created_at   timestamptz not null default now(),
  unique (workspace_id, email)
);

insert into system_config (key, value, secret, description) values
  ('invite_ttl_days', '7', false, 'Workspace invite link lifetime in days');
```

Accepting deletes the invite row. Emails are stored lowercase and trimmed (same `normEmail` as auth). Raw tokens are never stored: SHA-256 hash only, 256-bit URL-safe random token (same as reset links).

## API contract

Request bodies are FormData; responses JSON. "Signed in" = access token cookie; signed out → `401 {"error":"Not signed in."}`. Workspace-scoped endpoints answer `404 {"error":"Workspace not found."}` when the workspace is missing or the caller isn't a member.

Member JSON: `{"id": "...", "name": "Ada", "email": "ada@acme.com", "role": "admin"}`
Invite JSON: `{"id": "...", "email": "bob@acme.com", "invited_by": "Ada", "expires_at": "2026-11-06T10:00:00Z"}` (`invited_by` is `""` if that user was deleted)

| Method & path | Who | Fields | Success | Failures |
|---|---|---|---|---|
| `GET /api/workspaces/{slug}/members` | any member | — | `200 {"members": [member…], "invites": [invite…]}`; members admins first then by name; `invites` newest first, **always `[]` for non-admins**, expired ones included | 401, 404 |
| `POST /api/workspaces/{slug}/invites` | admin | `email` | `201 invite`. Re-inviting a pending email replaces its token and expiry and re-sends (still 201) | 400 `{"errors":{"email":"…"}}` (same messages as the shared `email` validator); 403 `{"error":"Only admins can invite people."}`; 404; 409 `{"errors":{"email":"They're already in this workspace."}}`; 503 `{"error":"Email is not configured. Contact your administrator."}` |
| `POST /api/workspaces/{slug}/invites/{id}/resend` | admin | — | `200 invite` with new token and expiry, email sent again | 403, 404 `{"error":"Invite not found."}` (also for an invite of another workspace), 503 |
| `DELETE /api/workspaces/{slug}/invites/{id}` | admin | — | `204` | 403, 404 `Invite not found.` |
| `GET /api/invites/{token}` | anyone (no auth) | — | `200 {"workspace": "Acme", "email": "bob@acme.com", "invited_by": "Ada"}` | 404 `{"error":"This invite link is invalid or has expired."}` |
| `POST /api/invites/{token}/accept` | signed in | — | `200 workspace` (the existing Workspace JSON, `role: "member"`); invite deleted. Already a member → same 200, invite deleted | 401; 404 as above; 403 `{"error":"This invite is for bob@acme.com. Sign in with that email to accept it."}` |

Order of checks for create/resend: membership + admin → validate → mail configured (503 before writing anything) → write → send. A send failure after writing is a 500 (the admin can resend).

Email (plain text, via `mail.Mailer.Send`):
- Subject: `{Inviter} invited you to {Workspace} on Taskwell`
- Body: `{Inviter} invited you to join {Workspace} on Taskwell.\n\nAccept the invite:\n\n{app_base_url}/invite?token={token}\n\nThe link expires in {N} days.\n`

## Backend (`backend/user`)

- Everything goes in `internal/workspace/`: routes in `routes.go`; handlers in `controller.go` (or a new `invite.go` if controller.go gets long); every query in `model.go`. Token helpers (`newToken`, `hash`) in `token.go` (copy of auth's, a few lines). Email normalization/validation: reuse the rules from `internal/auth/validate.go` by copying `checkEmail`/`normEmail` into `workspace/validate.go` (auth's are unexported; don't add a cross-package dependency for 10 lines).
- `workspace.New(pool, authenticate, mailer, cfg)`; wire in `cmd/user/main.go` with the same `mail.New(cfg)` instance auth uses.
- `GET /api/invites/{token}` must not require auth: the `wrap` helper always 401s, so give it a variant (e.g. `wrapPublic`) that skips authentication.
- Accept runs in one transaction: lock the invite row, check email against the user's email (`users.email`), insert membership `on conflict do nothing`, delete invite, return the workspace.
- Tests: add to `workspace_test.go` only what's pure (e.g. email check). Verify the rest with curl + Mailpit (`http://localhost:8025`, API `GET /api/v1/messages`).

## Frontend (user app)

Validation: the shared `email` validator from `@taskwell/shared/validate` (no new validators).

`api.ts`: add `Member`, `Invite` types.

Sidebar (`components/app/sidebar.tsx`): Settings group gets a third child, **Members** → `/$workspace/settings/members`.

| Route | File | Behaviour |
|---|---|---|
| `/$workspace/settings/members` | `routes/_app/$workspace/_main/settings/members.tsx` | `loader`: `GET /api/workspaces/{slug}/members`. Page title "Members", subtitle "People in {workspace name}." Admins: an invite form at the top (email input + "Send invite" button, inline; "Invite sent to x." status on success, form clears, list reloads via `router.invalidate()`). Members list: avatar (shared `Avatar`), name, email, role ("Admin"/"Member"), current user marked "(you)". For admins a "Pending invites" section: email, "Invited by Ada · expires in 5 days" (or "Expired"), "Resend" and "Cancel" buttons (secondary/ghost); resend shows "Sent." next to it, cancel removes the row. Non-admins don't see invites or the form. |
| `/invite?token=` | `routes/invite.tsx` (top level, outside `_app` and `(auth)`, works signed in or out) | `validateSearch` `{ token?: string }`. `loader`: `GET /api/invites/{token}` plus `getMe()` in parallel. Uses `AuthCard`. Title "Join {workspace}", subtitle "{invited_by} invited you to collaborate on Taskwell." (just "You've been invited…" if `invited_by` empty). States: **invalid** (404 or no token): `FormError` "This invite link is invalid or has expired." + link to `/`. **Signed out**: text "Sign in or create an account with {email} to accept." + "Sign in" (primary) → `/signin?redirect=<this url>` and "Create account" (secondary) → `/signup?redirect=<this url>`. **Signed in, email matches**: "Join workspace" button → `POST …/accept` → `router.invalidate()` then navigate to `/$workspace` of the returned slug. **Signed in, different email**: "You're signed in as {me.email}. This invite is for {email}." + "Switch account" button → `POST /api/auth/signout` then `/signin?redirect=<this url>`. |

`/signin` and `/signup`: `validateSearch` gains `redirect?: string`, accepted only if it starts with `/` and not `//` (open-redirect guard). Password, email-code and sign-up forms navigate to `redirect` (via `navigate({ href: redirect })`) instead of `/` when set. The `method`-tab switch on signin must keep `redirect`. `redirectIfSignedIn` should also honour it.

Sentence case everywhere. Forms: `noValidate`, shared validator first, then API, server `errors` onto fields, `error` via `FormError`, pending labels ("Sending…", "Joining…").

## Verification

- Backend: `go vet`, `go test`, then curl: invite (201, mail arrives in Mailpit), re-invite same email (201, new token, old link 404), invite existing member (409), member invites (403), non-member (404), list as admin vs member, resend, cancel, public GET by token, accept with wrong email (403), accept (200, membership exists, invite gone), accept twice (404), expired invite (set `expires_at` in psql → 404).
- Frontend: `npm run build`.
- Integration (main session): admin invites from Members page → Mailpit link → signed-out flow via sign-up with redirect → joins → lands in workspace; wrong-account flow; resend/cancel in UI.
