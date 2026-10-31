# Production readiness audit

Date: 2026-10-31. Scope: the whole repo (both Go services, the Postgres schema, both frontends, and the deploy setup). Three read-only reviews covered security, reliability and operations, and the frontend. Each finding was checked against the code; anything not confirmed is marked *needs check*.

**Status (2026-10-31): every P0, P1 and P2 item below has been fixed** (commits b7bf57d, 5e9a697, a49d73f, 76f34f8, 1f4debd and the P2 frontend commit after it), except the open items listed at the end. The original findings are kept for reference.

**Original verdict: not ready for production yet.** The features work and the foundations are good: parameterized SQL, workspace scoping on every query, rotating refresh tokens, transactional realtime events, and careful file serving. But there are no deploy artifacts, no CI, and no rate limiting. A few indexes are bypassed on hot paths. Several account-security gaps would be exploitable on the open internet. Everything in **P0** must be done before real users sign up. **P1** should land before the first paying customer.

Severity guide:
- **P0** blocks launch (security hole, data loss or outage risk).
- **P1** comes soon after launch.
- **P2** is hardening and polish.

---

## P0: must fix before launch

### Security

| # | Gap | Where | Failure scenario | Fix |
|---|---|---|---|---|
| S1 | **Emails are never verified, so accounts can be hijacked before the owner signs up.** Google sign-in and email-code sign-in attach to an existing account that has the same email. | `user/internal/auth/controller.go:54-78` (signup), `auth/model.go:164` (links `google_sub`), `model.go:223` (`useCode`) | An attacker signs up as `ceo@corp.com` with their own password. When the real CEO later uses "Continue with Google", they land in the attacker's account, and the attacker reads everything. `tether-admin -promote` on that email would also make the attacker a platform admin. | Verify the email at signup (code or link, no usable session before then). When Google or an email code proves ownership, clear any unverified `password_hash` and revoke its sessions. Store `email_verified_at`, and make `Promote` require it. |
| S2 | **No rate limiting anywhere.** | every route in `user/internal/auth/routes.go`, `admin/internal/auth/routes.go`, invites, uploads | Online password guessing, including against admins (password only, no MFA). Unlimited `otp/send` makes brute-forcing codes feasible. `password/forgot` and `otp/send` can be used to flood someone's inbox. Floods of bcrypt checks max out the CPU. | Token buckets per IP and per email on signin, signup, otp send/verify, forgot and admin signin. Count failed code attempts per email per time window, not per code. Lock out or back off per account. |
| S3 | **Go toolchain and dependencies have known vulnerabilities.** govulncheck reports 50 reachable stdlib issues. | `go.mod` in both modules (built with go1.24.1); `pgx v5.7.5`; `x/text v0.27.0` | Request smuggling behind a proxy, memory exhaustion through crafted cookies, Range or MIME headers, and CPU blowups in `mail.ParseAddress`. A pgx SQL injection applies only in simple-protocol mode, which PgBouncer setups often use (*needs check* for your deployment). | Build with a current Go release (add a `toolchain` line), upgrade to `pgx ≥ v5.9.2` and `x/text ≥ v0.41.0`, and run govulncheck in CI. |
| S4 | **WebSocket sessions outlive revocation.** Auth is checked once, at upgrade. | `user/internal/realtime/socket.go:101-131`, `issue/collab.go:160,170`, `auth/session.go:27` | A stolen 15-minute access cookie opens `/api/realtime` and keeps it alive with pings. Sign-out, password reset and suspension don't close it, and the holder can keep joining issues and receiving or writing live descriptions. `canEdit` is fixed at join, so a role downgrade doesn't take effect. | Close the socket when the token expires (the client reconnects with a refreshed token). On sign-out, reset, password change or suspension, publish a user-wide `session.revoked` event that closes their sockets. Re-check suspension on `collab.join`. Recompute `canEdit` on `workspace.permissions`. |
| S5 | **CSRF relies only on SameSite=Lax.** | cookies in `auth/session.go:95-98`; every write is a simple FormData POST | Login CSRF: a hostile page submits sign-in with the attacker's credentials, and the victim then works inside the attacker's account. Forced sign-out also works. Any sibling subdomain counts as same-site and can do full CSRF. | Reject non-GET `/api/*` requests unless `Sec-Fetch-Site` is `same-origin` (or `Origin` equals `app_base_url`), or require the `X-Tether-Client` header (which forces a preflight). Use `__Host-` cookie names. |
| S6 | **Live (Yjs) edits bypass document validation.** | `issue/collab.go:253-264` stores and relays update and compact bytes unchecked; `rich-editor.tsx:42-50` checks image `src` only on paste | A user who can edit injects `<img src="https://attacker/px">` through a raw Yjs update. Every viewer's browser requests it, leaking IP, user agent and viewing time. Honest clients' saves then fail `checkDoc` silently, so the saved description stops updating. Storage can also grow without limit (1 MB frames). | Enforce the `/api/files/…` rule in the Image `renderHTML` (and keep the link allowlist). Add a CSP `img-src 'self' data: blob:`. Cap update size and rate per connection and total size per issue. |

### Reliability and data

| # | Gap | Where | Failure scenario | Fix |
|---|---|---|---|---|
| R1 | **`id::text = $1` bypasses the primary and foreign key indexes on hot paths.** EXPLAIN confirms sequential scans. | `issue/model.go:157,235,460,477,483,536,721,1180,1186,1192,362-364,710,927`, `notify/model.go:413` | Each issue update runs at least 5 full scans of `issues` plus one of `issue_doc_updates`, and the scans hold `FOR UPDATE`. At around 1M issues every edit takes seconds and requests pile up. | Compare uuid to uuid (`where id = $1`, validating ids in Go). Fix every `::text =` and `::text = any`. Add a test that runs EXPLAIN on these queries. |
| R2 | **No HTTP timeouts and no graceful shutdown.** | `user/cmd/user/main.go:83`, `admin/cmd/admin/main.go:164` | Slowloris (slow headers or slow uploads) holds goroutines and file descriptors. On deploy, SIGTERM cuts transactions mid-flight. The email worker can be killed after sending and before commit, so the email goes out twice. Presence rows linger for 90 s. | `http.Server{ReadHeaderTimeout, IdleTimeout, MaxHeaderBytes}`, with per-handler deadlines through `http.ResponseController` (a global WriteTimeout would break WebSockets). Use `signal.NotifyContext`, then `srv.Shutdown`, and pass that context to the listeners and the worker. |
| R3 | **Emails are sent over SMTP with no timeout, inside a database transaction.** | `mail/mail.go:57`, `notify/model.go:356,410`, `auth/controller.go:177`, `workspace/invite.go:277` | One stalled SMTP server hangs the worker for good: no emails go out, no error is logged, and a connection sits idle in a transaction holding a row lock. OTP, password-reset and invite requests hang too. | Dial with a timeout and set a deadline using `smtp.NewClient`. Restructure as claim, commit, send, then mark sent. Put auth and invite emails on the same queue table. Set `idle_in_transaction_session_timeout`. |
| R4 | **Bad addresses block the email queue.** | `notify/worker.go:54-65`, `model.go:347` | Each run stops after 5 failures, starting again from the oldest rows. Five bouncing addresses at the head of the queue mean no one else ever gets an instant email. | Add `attempts` and `next_attempt_at` with exponential backoff, and log and park a row after N tries. |
| R5 | **Live-editing compaction can delete an edit that was never merged** (*needs a load test*). | `issue/collab.go:221`, `model.go:811-821`, `collab.ts:74`; `issue/listen.go:99` (relay skips missing rows) | Ids are assigned at insert time, not commit time. A client compacts up to id 15 while id 14 is still uncommitted, so 14 is deleted without being merged. Later viewers miss that text, and the next save overwrites the stored description. | Compact with `delete … where id = any($idsActuallySent)`. In relay, send small updates inline in the NOTIFY and close the peer on a miss, so it reloads instead of diverging. |
| R6 | **Unbounded issue lists include full descriptions, and every live save makes every viewer refetch them.** | `issue/model.go:204,209,115` (`issueCols`), `realtime/event.go:82`, `model.go:851` | With a 5,000-issue project, each list response is several MB. While anyone types, every open tab on that project re-downloads the whole list about once a second. | Drop `description` from list columns and serve it only from `get`. Add keyset pagination. Don't publish `ProjectIssues` for saves that only change the description. Throttle `/projects/*/issues` refetches on the client to 2–5 s. |
| R7 | **The database pool is sized implicitly, and the listeners query through it.** | `db/db.go:20`, admin `main.go:28`, `realtime/listen.go:54`, `issue/listen.go:77` | The default is `max(4, NumCPU)` connections, shared by requests, every keystroke, awareness notifications and the worker. Under load, `Acquire` waits with no timeout and the listeners block. Postgres's notification queue then fills, so every `pg_notify` inside a write fails and every write returns 500. | Set `MaxConns` from an env var and budget it against `max_connections`. Add `statement_timeout` and a per-request context timeout. Hand listener work to a bounded worker instead of querying inline. Alert on `pg_notification_queue_usage()`. |

### Deploy and operations

| # | Gap | Fix |
|---|---|---|
| O1 | **There is no way to ship it:** no Dockerfile, no reverse-proxy config, no CI. Both SPAs assume `/api` and `/api/realtime` are on the same origin, and nothing in the repo serves `dist` with a history fallback. | Add a Dockerfile for each Go service (multi-stage, distroless, non-root) and a Caddy or nginx config. The proxy should serve both SPAs with a history fallback, cache `/assets/*` as immutable and `index.html` as no-cache, proxy `/api` with WebSocket upgrade, and set security headers (O2). Add CI that runs `go vet`, `go test -race`, govulncheck, `npm run build`, the `*.check.ts` scripts and `npm audit`. |
| O2 | **No security headers** for the SPA or the JSON API (no CSP, HSTS, frame-ancestors or `nosniff`). Only file downloads have them. | Set them at the edge: CSP (`default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'` because ProseMirror sets inline styles; `connect-src 'self' wss:`), HSTS, `frame-ancestors 'none'`, `Referrer-Policy`. Set `no-store` on auth and `/api/me` responses. |
| O3 | **Unsafe config defaults.** A missing `DATABASE_URL` silently falls back to `tether:tether@localhost sslmode=disable`. `app_base_url` defaults to `http://localhost:5173`, which also makes session cookies non-Secure (`auth/session.go:124`). | Require `DATABASE_URL` and use `sslmode=verify-full` in production (an `APP_ENV` switch). Refuse to start with a non-https `app_base_url` outside dev, or take the Secure flag from env. Log the effective config at startup. |
| O4 | **No backups** of Postgres or `UPLOAD_DIR`, and no restore runbook. Uploads sit on local disk with no fsync before commit. | Use managed Postgres with point-in-time recovery, or scheduled `pg_dump` plus WAL archiving. Move files to S3-compatible storage, or at least put them on an absolute, required, backed-up volume and call `Sync()` before rename. Write and test a restore runbook. |
| O5 | **Secrets are stored in plain text in the database.** One `jwt_secret` signs both user and admin tokens, and `smtp_password` and `google_client_secret` sit next to it. | Load `JWT_SECRET` and a separate `ADMIN_JWT_SECRET` from env or a secrets manager. Treat the database and its backups as containing secrets. |

---

## P1: soon after launch

### Security
- **Role managers can weaken roles more powerful than their own.** `workspace/roles.go:47-104` checks only the new permissions. Someone with only `roles.manage` can empty or delete the "Lead" role, or lower the Member role. Return 403 unless the caller holds every permission the role currently has.
- **Invites act as a spam and phishing relay.** `workspace/invite.go:41-81` allows unlimited invites from an unverified account, with a subject line the attacker controls, sent from your domain. Rate-limit invites per user and per workspace each day, require a verified email to invite, and keep free-form names out of the subject.
- **Access tokens can't be revoked** (`auth/session.go:27`). Add a `session_version` claim, or at least check `suspended_at` in `Authenticate`.
- **A first password can be set without re-authenticating** (`auth/controller.go:251`), which turns a stolen cookie into permanent access. Require an email code, and email the user when a password is added or changed.
- **Admin hardening.**
  - Admin sessions are stateless and last 8 h, and survive sign-out. Add a sessions table or a short TTL.
  - There is no MFA, no IP allowlist and no audit log. Add MFA, and record config changes, suspensions and promotions in an append-only log.
  - Changing `app_base_url` redirects every reset and invite link, so a compromised admin can take over any account.
- **Resource caps.** Limit WebSocket connections per user, set an upload quota per workspace (today any member can fill the disk 10 MB at a time), and cap search query length.

### Reliability
- **Data retention.** `refresh_tokens`, `auth_tokens` and `notifications` are never purged. `issue_doc_updates` only compacts when someone opens an issue that has more than 100 updates. Files are orphaned when a workspace, issue or attachment is deleted. Add a daily sweeper behind `pg_try_advisory_lock`.
- **Migrations run at startup in one transaction, with no down migrations.** Plain `create index` (for example the trigram indexes in `016`) locks writes. Destructive steps break old instances during a rolling deploy. The admin service never checks the schema. Run migrations as a separate step, use expand/contract for destructive changes, put `create index concurrently` in its own non-transactional migration, and set `lock_timeout`. Note that `uuidv7()` requires Postgres 18 or later.
- **Observability.**
  - slog writes text to stderr, and there is no access log, request id, metrics or tracing.
  - `/healthz` returns 200 even when the database is down.
  - Fix: write JSON logs to stdout, add request-id and access-log middleware, add `/readyz` that pings the database and checks the listeners, and expose metrics for pool stats, WebSocket count, email queue depth and age, and NOTIFY queue usage.
- **`jwt_secret` is read from the database on every request,** and each email makes 5 config queries (`auth/session.go:95`, `mail.Send`). Cache sysconfig for about 10 s, or invalidate the cache through the existing NOTIFY.
- **NOTIFY on every keystroke and cursor move** (`collab.go:247`, `model.go:760`). NOTIFY commits serialize on a global lock, so 100 concurrent editors produce over 1,000 commits a second. Coalesce awareness to one every 250 ms and batch updates every 50–100 ms per connection.
- **Missing indexes on foreign keys** slow down cascades: `notifications(workspace_id)`, `users(avatar_file_id)`, `workspace_invites(role_id)`, `project_key_aliases(project_id)`, plus the `*_by` and `actor_id` and `author_id` columns. Deleting a workspace is one large transaction.
- **Admin stats get slower over time.** They are full-table counts on unindexed predicates (`admin/internal/stats/model.go:38-62`), and they re-run on every issue or comment create. Use range predicates with an index on `created_at`.
- **Seed lease of 10 s** (`issue/model.go:793`). If the seeding client is slow, a second instance can seed as well, and the description is duplicated. Make the lease renewable.
- **A REST description update deletes live updates while people are editing** (`model.go:460`) (*needs check*). Close that issue's live sessions so editors reload.

### Frontend
- **"Try again" on route errors does nothing** (`shared/.../boundaries.tsx:97`). Call `router.invalidate()` before `reset()`.
- **A paste over 1 MB starts an endless reconnect loop.** The server closes the socket because the frame is too big; the client reconnects, re-sends and is closed again, every second (`realtime.ts:300`, `collab.ts:175-185`). Guard the update size in collab, reset the backoff only after the socket has stayed up, and add jitter.
- **Old tabs break after a deploy,** because lazy chunks get new hashes. Reload on `vite:preloadError` in both `main.tsx` files.
- **Sign-in loses where you were going:** `_app.tsx:22` redirects without `?redirect=`, so a link from an email lands on `/`. Google sign-in also loses the invite redirect (`auth-card.tsx:43`); carry it through the OAuth state.
- **A session that expires mid-use shows an error page** instead of going to sign-in (`api.ts:104-110`). Redirect when the refresh fails.
- **Signing out doesn't reach other tabs,** which keep showing data and their socket. Post on a `BroadcastChannel('tether-auth')`.
- **Live description saves fail without telling anyone.** There is no length check and the server sends no error frame, so the live doc and the saved JSON drift apart. Validate the length on change and send a save-error frame.
- **Unsynced offline edits and comment drafts are lost when the tab closes.** Add a `beforeunload` prompt while there are unsent changes.
- **The editor bundle is pulled into the list views.** `issue-view.tsx:12` imports `issuePath` from `issue-form`, and `projects/$key.tsx:198` imports the create form statically. The result is about 590 kB (190 kB gzipped) on pages that never show an editor. Move `issuePath` to `api.ts` and lazy-load the dialog.
- **Moving one card refetches the whole app,** because three places call `router.invalidate()` without a filter (`issue-view.tsx:68`, `command-palette.tsx:104`, `timeline.tsx:206`). Pass a filter.
- **Reconnect storms after a restart.** Backoff has no jitter, and every tab refetches everything on `hello` at the same moment. Add a random factor and spread the resync over a few seconds.
- **iOS zooms on every input,** because the inputs use 13.3px text and iOS zooms anything under 16px. Use 16px on iOS (`input.css:5`).

---

## P2: hardening and polish

- **Account enumeration through signup's 409** (`auth/controller.go:65-67`). Answer both cases the same way once S1 is in.
- **Performance at scale.**
  - Every comment is a full Tiptap editor (200 comments means 200 ProseMirror views); render read-only docs with a light JSON renderer.
  - Lists and the board are not virtualized, and every board drag re-renders every card; memo the rows and cards.
  - The timeline and the members list are unbounded; paginate them past a few hundred items.
- **Accessibility.**
  - Touch targets below 44px: dialog close, sidebar items, autocomplete options, date picker.
  - The input focus ring is under 3:1 contrast.
  - `--link #5E6AD2` on the background is about 4.2:1, below 4.5:1.
  - Single-key shortcuts can't be turned off (WCAG 2.1.4).
  - Dialogs are missing `aria-describedby`.
  - There is no keyboard way to move cards on the board.
- **Design fidelity.** Inter Variable and Berkeley Mono are never loaded, so the UI uses system fonts and the 510/590 weights are approximated. There is no `color-scheme: dark`, so native controls render light.
- **Dates.** `relativeTime` hard-codes en-US while other dates use the browser locale. The digest hour is a single global UTC hour with no per-user timezone.
- **Smaller fixes.**
  - `safeRedirect` accepts control characters such as `/\t/evil.com`. It isn't exploitable today; add a character check and a test case.
  - The invite loader accepts the invite inside a loader, so it would also run on hover if anything ever linked to it.
  - Offline gets the same message as every other error.
  - The realtime path map is never pruned.
  - There is no declared browser baseline; the code needs Safari 15.4 or later.
- **Smaller backend fixes.**
  - Signup isn't atomic: the user and the default workspace are created in separate transactions.
  - Awareness frames over 7.9 KB are dropped across instances.
  - `notify.Record` makes one query per recipient.
  - The listener reconnect closes every socket at once.
- **Tests.**
  - Go coverage is 8–20% per package, every SQL query is untested, and no test touches Postgres. Add integration tests against a throwaway database (for example a docker Postgres in CI) for the model packages.
  - The frontend has only `*.check.ts` scripts, which nothing runs automatically. Wire them into CI and turn the scratchpad Playwright scripts into a committed e2e suite.
- **Dead code and stale docs.**
  - `usernameOrEmail` and `issuePriority` are unused.
  - The `ponytail:` note in `timeline.tsx:22` is outdated, since the timeline is realtime now.
  - `docs/rich-text-plan.md` still describes the separate collab socket.
  - `docker-compose.yml` exposes Postgres on 0.0.0.0 with the default credentials (fine for dev only).

---

## What's already solid

- **SQL:** every query is parameterized, and LIKE input is escaped.
- **Workspace scoping:** every query is scoped by workspace, and non-members get 404 rather than 403.
- **Access rules:**
  - You can't grant permissions you don't hold.
  - You can't manage members who outrank you.
  - A workspace always keeps at least one admin, enforced under a row lock.
- **Refresh tokens:** stored hashed, rotated, with reuse detection, a grace window for races between tabs, and revocation of the whole session family. Their cookie is scoped to `/api/auth`.
- **Codes and links:** email codes, reset links and invite tokens are hashed and single-use.
- **JWT:** the algorithm, issuer and audience are pinned, and admin tokens and user tokens can't be swapped for each other.
- **Google OAuth:** uses PKCE and checks state in constant time.
- **Sign-in:** timing is equalized so it doesn't reveal which emails exist.
- **Files:**
  - Stored under server-generated ids.
  - The file type is detected by the server.
  - Only PNG, JPEG, GIF and WebP are shown inline; everything else downloads.
  - Responses carry `sandbox` CSP and `nosniff`.
  - Uploads stream to disk with size caps.
- **Rich text on save:** `checkDoc` allowlists node and mark types, link schemes and image sources. The frontend has no `dangerouslySetInnerHTML` anywhere.
- **Realtime:**
  - Events are published inside the write's transaction and delivered only on commit.
  - Payloads are size-capped and collapse to a workspace prefix.
  - Listeners reconnect and resync clients.
  - Slow consumers are dropped rather than blocking.
  - Ping/pong detects dead sockets, which helps on sleeping iPads.
  - It works across instances without sticky sessions.
- **Email worker:** `FOR UPDATE SKIP LOCKED` makes it correct with several instances.
- **Issue numbering** is gap-free under a row lock.
- **Body limits:** every route has a `MaxBytesReader`.
- **Frontend:**
  - Token refresh is single-flight.
  - Redirects are validated.
  - Dialogs are native `<dialog>` with focus trap and restore.
  - The palette and autocomplete follow the ARIA combobox pattern.
  - Validators match the backend's limits.
  - `npm audit` is clean, the lockfile is committed, and route code splitting is on.

---

## Suggested order

1. **Week 1 (security):**
   1. S3: toolchain and dependency upgrades.
   2. S2: rate limiting.
   3. S1: email verification.
   4. S5: CSRF check.
   5. S4: socket revocation.
   6. S6: image `renderHTML` and CSP.
   7. The roles fix.
2. **Week 1 (data):**
   1. R1: uuid comparisons. Mechanical, with a big payoff.
   2. R2: timeouts and graceful shutdown.
   3. R3 and R4: email queue claim, retry and backoff.
3. **Week 2 (ship):**
   1. O1: Dockerfiles, proxy config and CI.
   2. O2: security headers.
   3. O3: config guards.
   4. O4: backups and S3.
   5. O5: secrets in env.
   6. `/readyz` and JSON logs.
4. **Week 2 (scale):**
   1. R6: lean, paginated lists, and no list refetch per keystroke.
   2. R7: pool sizing and timeouts.
   3. R5: exact-id compaction.
   4. The 1 MB reconnect loop.
5. **Then:** the rest of P1, followed by P2 as usage grows.

---

## Open items after the fixes

- **Fonts:** Berkeley Mono is commercial and isn't bundled; code text uses the system monospace fallback until a licensed copy is added to `frontend/shared/src/fonts/`.
- **Invite e2e leftovers:** there's no API to delete a user, so each invite e2e run leaves one `invite-<timestamp>@tether.test` account with no workspace.
