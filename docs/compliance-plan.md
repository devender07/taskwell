# Compliance plan: DPDP, GDPR, SOC 2

Research (three agents, Nov 2026) mapped India's DPDP Act 2023 + DPDP Rules 2025, EU/UK GDPR and SOC 2 to the code.
Already in place: MFA, sessions, rate limits, CSRF/CSP, tenant isolation, workspace export, workspace audit log (1 year),
admin audit (append-only), sealed integration/webhook/MFA secrets, retention sweeps, trash. Cookies are all strictly
necessary and analytics is cookieless, so **no cookie banner**.

Taskwell is a **controller / data fiduciary** for account data (name, email, sign-in, billing link, security logs) and a
**processor** for workspace content (issues, comments, chat, files), which belongs to the workspace.

## Scope (this round)

1. Self-serve account deletion (GDPR Art. 17, DPDP s.12, SOC 2 C1.2).
2. Personal data download (GDPR Art. 15/20, DPDP s.11).
3. Account security log: sign-ins, failures, password/email/MFA/token changes, kept 1 year (DPDP Rule 6, SOC 2 CC7.2).
4. Email on API token creation (SOC 2 CC7.3).
5. Terms acceptance + 18+ at sign-up (DPDP s.9, GDPR Art. 8), stored as `users.terms_accepted_at`.
6. `workspace_audit` append-only (SOC 2 CC7.2 log integrity).
7. Retention: unread notifications 1 year, closed browser sessions 30 days, account events 1 year.
8. Public `/privacy`, `/terms`, `/subprocessors` pages (draft legal text, lawyer review before launch), linked from the
   landing footer, sign-in and sign-up.
9. Ops: security headers on the API hosts (Caddyfile); docs/compliance/ with the breach runbook, records of processing
   and the SOC 2 process checklist.

Not this round (say so, don't scaffold): workspace "require MFA", idle refresh timeout, re-auth on token creation,
sealing secret `system_config` values, breached-password check, off-box log shipping, inactive-account purge.

## Schema (migration 034_compliance.sql)

```sql
alter table users add column terms_accepted_at timestamptz;   -- null for accounts made before this
-- pending sign-ups get the same column if the account row is created at verify time

create table account_events (
  id uuid primary key default gen_random_uuid(),
  user_id uuid not null references users(id) on delete cascade,
  action text not null check (action in ('sign_in', 'sign_in_failed', 'sign_out', 'password_changed', 'password_added',
    'email_changed', 'mfa_enabled', 'mfa_disabled', 'token_created', 'token_deleted')),
  ip inet, user_agent text not null default '',
  detail text not null default '',          -- e.g. the token's name, "google", "password", "code"
  at timestamptz not null default now()
);
create index on account_events (user_id, at desc);
```

`workspace_audit` trigger: block UPDATE except the FK action that nulls the actor when a user is deleted; block DELETE
except rows older than 1 year (the sweeper) or rows whose workspace is gone (workspace delete cascade). Add a test.

Sweeps (`sweep/model.go` retention list): `account_events at < now() - 1 year`; `notifications created_at < now() - 1 year`;
`browser_sessions` closed more than 30 days ago (check the table's columns).

## API contract (user API, FormData in, JSON out, errors as elsewhere: `{error}` or `{errors:{field}}`)

### Account deletion
- `POST /api/me/delete/code` → 204. Emails a 6-digit code (new purpose `account_delete`, `otp_ttl_minutes`, same
  limiter and `sendSecret` as `passwordCode`). HTML email: heading "Confirm account deletion", note that it can't be undone.
- `POST /api/me/delete` field `code` →
  - 400 `{errors:{code}}` bad/expired code ("That code is wrong or has expired.").
  - 409 `{error, workspaces:[{slug, name}]}` when the user must act first:
    - they're the only member with `workspace.delete` (or the only admin) of a workspace that has other members:
      "Make someone else an admin of these workspaces, or remove the other members, first."
    - a workspace where they're the only member still has an active paid subscription: "Cancel the plan for these
      workspaces first." (Paddle is never called from here.)
  - 204 on success, with the auth cookies cleared like sign-out. In one transaction: delete the workspaces where they
    are the only member (same model call as `DELETE /api/workspaces/{slug}`), then `delete from users`. FKs already
    pseudonymise shared content (`on delete set null`, shown as "Deleted user"). After commit, a plain email to the old
    address: "Your Taskwell account was deleted".
  - Notes they own are deleted with them (existing cascade); say so in the dialog.

### Personal data download
- `GET /api/me/export` → 200 JSON, `Content-Disposition: attachment; filename="taskwell-my-data.json"`:
  `{exported_at, profile:{id,name,email,created_at,terms_accepted_at,mfa_enabled,has_password,google_linked},
  workspaces:[{slug,name,role,joined_at}], notification_settings:{...}, api_tokens:[{name,created_at,last_used_at}],
  sessions:[{created_at,last_used_at,ip,user_agent}], account_events:[{action,detail,ip,user_agent,at}]}`.
  Never secrets or hashes. Workspace content is not included (the workspace's admins export that). Limiter: 10/hour.

### Security activity
- `GET /api/me/account-events?before=<id>` → `{events:[{id,action,detail,ip,user_agent,at}], has_more}`, 50 per page,
  newest first. Only the caller's own.
- Recorded at: sign-in success (password, code, Google, MFA step finished), failed password/code/MFA sign-in for an
  existing account (no event for unknown emails), sign-out, password changed/added, email changed, MFA on/off, token
  created/deleted. Record in the change's own transaction where one exists; otherwise best effort, logging the error
  without failing the request.

### Token creation email
- After `POST /api/me/tokens` succeeds: email "A new API token was created" with the token's name and "If this wasn't
  you, delete it in Settings → API tokens and reset your password."

### Terms at sign-up
- `POST /api/auth/signup` gains required field `accept_terms` = `true`; otherwise 400
  `{errors:{accept_terms:"Confirm you're 18 or older and accept the terms."}}`. Stored as `terms_accepted_at` when the
  account is created. Google sign-up sets it too (the button sits under the "By continuing…" line).
- `GET /api/me` gains `terms_accepted_at`.

## Frontend (user app)

- Public routes `/privacy`, `/terms`, `/subprocessors` (outside `_app`, no auth), one shared `LegalPage` layout in
  `src/components/legal/`, readable width (max 720px), "Last updated" date, links between them. Text from the facts
  below; every unknown is a visible `[placeholder]`.
- `landing.html` footer: Privacy, Terms, Subprocessors, contact `privacy@taskwell.dev`.
- Sign-in and sign-up: caption under the buttons "By continuing, you agree to the terms and the privacy notice." (links).
  Sign-up form: required checkbox "I'm 18 or older and accept the terms and privacy notice", sent as `accept_terms`,
  validated client-side too.
- Settings → Profile, new section "Your data":
  - "Download my data" button → `GET /api/me/export`, saved as a file.
  - "Delete account" (danger) → dialog: explains what goes (account, workspaces where you're the only member, your
    notes) and what stays (your issues/comments in shared workspaces, shown as "Deleted user"); "Send code" → code input
    → "Delete account". 409 lists the workspaces with links. Success → `/signin`.
- Settings → Profile, section "Security activity": the last events (action in words, detail, IP, browser, relative
  time), "Show more" pages with `before`.

## Facts for the legal pages

- Controller: `[Company legal name]`, `[Registered address, India]`. Contact and grievance officer:
  `[Name]`, privacy@taskwell.dev; reply within 30 days (DPDP cap 90). Complaints: Data Protection Board of India; EU/UK
  users may complain to their supervisory authority. EU/UK representative: `[to appoint]`.
- Data and purposes: account (name, email, password hash, Google id) to provide the service (contract / legitimate use);
  security logs (IP, browser, sign-ins, audit log) for security (legitimate interest), kept 1 year; billing via Paddle
  (merchant of record, its own controller); emails we send (codes, notifications; settings let you turn them off). No
  marketing email, no ads, no selling data, no tracking cookies.
- Cookies: only strictly necessary (sign-in session, refresh, 2FA step, sign-up nonce, Google sign-in state). Local
  storage: theme and UI preferences. Analytics: Cloudflare Web Analytics, cookieless.
- Retention: account until you delete it; security log and audit log 1 year; trash 30 days; read notifications 90 days,
  others 1 year; webhook deliveries 30 days; backups 14 days (deleted data can stay in backups up to 14 days).
- Rights: access/download (Settings → Profile), correction (profile, email change), deletion (Settings → Profile),
  nominate someone (email us), withdraw consent (delete the account), grievance (email). Workspace content: ask the
  workspace's admins (they control it; we act on their instructions).
- Sub-processors: Cloudflare (hosting of the web app, file storage R2, CDN, analytics; global); `[VPS host]`
  (API, database, backups; `[country]`); `[SMTP provider]` (email; `[country]`); Anthropic (AI testing, only when a
  workspace uses it; US; API data not used for training). Integrations you enable (Google sign-in, GitHub, GitLab,
  webhooks) receive data at your request. Paddle is an independent controller for payments.
- Breach: we notify affected users and the Data Protection Board of India without delay (details within 72 hours), EU
  supervisory authorities within 72 hours where required, and affected workspace admins without undue delay.
- Age: 18+ only, business use. Transfers: data may be processed outside India/EU under the sub-processors' safeguards
  (SCCs / DPF).
- Terms: draft standard SaaS terms (accounts, acceptable use, customer content ownership, our processing as processor
  per the DPA available on request, fees via Paddle, termination and export, disclaimers, liability cap, governing law
  `[India, courts of ___]`).

## Checks

- Backend: tests for delete (blocked by shared workspace, blocked by paid solo workspace, success pseudonymises a
  comment), export shape, account events recorded on sign-in/failure, workspace_audit trigger, signup without terms.
- Frontend: `npm run build`; click through sign-up with the checkbox, download, delete account, legal pages.
