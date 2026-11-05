# End-to-end checks

Browser scripts that drive the real apps against a running stack. They use the `playwright-core` and Chrome already installed on the machine, so the repo gains no dependency.

| Script | Covers |
|---|---|
| `user.mjs` | Password sign-in, quick-create (`C`) with a mention and assignee, the notifications bell updating live and its popover opening the issue, palette search, live description editing between two users with presence and named carets, a comment with an image attachment, the JSON renderer matching the editor's DOM, the offline message, board drag and Shift+←/→ keyboard moves (announced, focus kept), turning single-key shortcuts off and on, timeline paging ("Show older activity"), Home blocks (assigned issue, recently viewed, live activity), chat (a direct message arriving live with an unread pill that clears on opening, a thread reply, a reaction) |
| `invite.mjs` | Inviting a fresh address from the members page, the invite and sign-up code read from Mailpit, signing up from the invite link, "Accept invite" landing in the workspace as a member |
| `admin.mjs` | Admin sign-in with password and a TOTP code (wrong code refused), redirect back, realtime socket, sign-out |

## Run

Start Postgres, both APIs and both Vite dev servers (user on :5173, admin on :5174) with the demo seed loaded, then from `frontend/`:

```sh
E2E_ADMIN_TOTP_SECRET=<setup key> npm run e2e
# or one at a time
node e2e/user.mjs
node e2e/invite.mjs
E2E_ADMIN_TOTP_SECRET=<setup key> node e2e/admin.mjs
```

Each line prints `PASS`, `FAIL` or `SKIP`, and the process exits non-zero on any failure. On a crash, screenshots go to `$E2E_OUT` (default: the OS temp directory). `HEADED=1` shows the browser.

`user.mjs` creates one temporary issue (with about 110 comments for the paging check) and deletes it at the end, and deletes its chat messages (they stay as "This message was deleted." in the demo/Maya direct chat, since deletes are soft); the two images it uploads become unreferenced and the API's daily sweep removes them. It signs in twice (once through the form, once over the API). Sign-in is rate-limited per IP, so leave a little time between runs.

`invite.mjs` signs demo in once over the API and signs up one new account (`invite-<timestamp>@taskwell.test`), reading both emails from Mailpit. At the end demo removes that member; the account itself stays, in no workspace, since the API has no way to delete a user.

## Settings

| Variable | Default |
|---|---|
| `PLAYWRIGHT_CORE` | `/home/devender/.nvm/versions/node/v22.14.0/lib/node_modules/@playwright/cli/node_modules/playwright-core/index.mjs` (any `playwright-core` `index.mjs`) |
| `CHROME_PATH` | `/usr/bin/google-chrome` |
| `E2E_USER_URL`, `E2E_ADMIN_URL` | `http://localhost:5173`, `http://localhost:5174` |
| `E2E_WORKSPACE`, `E2E_PROJECT` | `acme`, `ENG` |
| `E2E_EMAIL`, `E2E_EMAIL_2`, `E2E_PASSWORD` | the seeded `demo@taskwell.test` (workspace admin and platform admin), `maya@taskwell.test` (member), `Demo12345` |
| `E2E_MAILPIT_URL` | `http://localhost:8025` |
| `E2E_ADMIN_TOTP_SECRET` | none. This is the base32 setup key shown when the admin account enrolled in MFA. Without it, `admin.mjs` skips. Keep it out of the repo. |

`lib.mjs` has a small TOTP generator (RFC 6238, SHA-1, 30 s). A code is accepted only once, so if a recent run used the current one, `admin.mjs` waits for the next 30-second window.
