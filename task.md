# Taskwell feature list

Task management for SaaS teams. Build in phase order; don't skip ahead (see `CLAUDE.md` → Product scope).

Legend: `[x]` done, `[ ]` to do.

## Phase 1: Accounts and workspaces

- [x] Email + password sign-up and sign-in, email-code sign-in, forgot/reset password
- [x] JWT access token + rotating refresh token, Remember me, sign-out
- [x] Continue with Google (code done; add `google_client_id` / `google_client_secret` in `system_config` to turn it on)
- [x] Workspaces: default one created at sign-up, create dialog, settings page with sidebar, admin delete, name + URL slug (`taskwell.dev/acme`), switcher, settings (rename, change URL), admin/member role on membership
- [x] Team invites: invite by email from Settings → Members, accept link (`/invite`, email must match), resend or cancel; member list
- [x] Invite prompt (docs/invite-prompt-plan.md): sign-up never auto-joins; pending invites show a "Join <workspace>?" prompt, "Not now" leaves a Join row in the Taskwell bot; pending invites listed with Join in the workspace switcher
- [x] Roles: built-in Admin and Member plus custom roles with permissions (Settings → Roles), change role, remove member, leave
- [x] Profile: name, change or set password (Settings → Profile; signs out other devices)
- [x] Avatar upload (Settings → Profile; initials when none)

## Phase 2: Projects and issues

- [x] Projects: name, key (`ENG`), description, archive (sidebar → Projects; `projects.manage` permission)
- [x] Issues: title, rich text description (Tiptap), status, assignee, priority, due date, labels
- [x] Readable issue IDs (`ENG-142`) used in URLs (`/{workspace}/issue/ENG-142`)
- [x] Default statuses (Backlog, Todo, In progress, Done, Cancelled), editable per workspace (Settings → Workflow; labels in Settings → Labels)
- [x] Sub-issues (one level, same project) and "blocks / blocked by" links; issue search by ID or title

## Phase 3: Views

- [x] List view with grouping (status, assignee, priority) and sorting
- [x] Kanban board with drag between columns (pointer events: mouse and touch, edge auto-scroll)
- [x] Manual issue order (`docs/issue-order-plan.md`): Manual sort, drag to reorder in list and board (across columns too), Alt+↑/↓, shared and live; MCP `reorder_issues`, `list_issues` sort, `create_issue` after/before
- [x] Filters by assignee, status, label, priority, kept in the URL so they can be shared
- [x] "My issues" view, with an Assigned / Watching switch
- [x] Member analytics (`docs/member-analytics-plan.md`): a member page (workload, completed per week, median cycle time, activity, testing, current cycles) and a Team page; own numbers for everyone, others with `analytics.view`; counts only projects the viewer can see
- [x] Creating workspaces is the role permission `workspaces.create` (group Workspace): allowed when a role in any of the user's workspaces grants it (Admin always), or when they're in no workspace yet; otherwise the Create workspace option is hidden

## Phase 4: Collaboration

- [x] Rich text descriptions (Tiptap): formatting toolbar, markdown shortcuts, live collaborative editing with named cursors (Yjs relayed and stored by the Go API, across instances via Postgres LISTEN/NOTIFY)
- [x] Comments on issues (rich text, reusing the shared editor; edit and delete; `comments.create` / `comments.manage`)
- [x] Activity log (status changes, reassignments, edits)
- [x] `@mentions` in comments and descriptions, notified by email
- [x] Image and file attachments in descriptions and comments (S3 or compatible, configured in `system_config` `s3_*`, served by presigned redirect; paste, drop or the attach button)

## Phase 5: Keyboard-first

- [x] Command palette (`⌘K`): jump to anything, run any action
- [x] Quick-create issue (`C`) from anywhere
- [x] Fast search across issues and projects
- [x] Single-key shortcuts on an issue: `A` assign, `S` status, `P` priority, `L` label

## Phase 6: Notifications

- [x] In-app inbox: mentions, assignments, status changes on watched issues
- [x] Email notifications with per-user settings and a daily digest option
- [x] Watch / unwatch an issue

## Realtime

- [x] One WebSocket per tab: every write publishes (in its transaction, via Postgres NOTIFY) which REST paths changed; open tabs refetch just those
- [x] Live description editing on the same socket; "also viewing" presence on issues
- [x] Admin app updates live (sign-ups, workspaces, stats, suspensions, config)

## Home

- [x] Personal dashboard: greeting, needs attention (due this week, overdue in pink), assigned to me, unread inbox, live workspace activity, recently viewed, project progress

## Chat

- [x] Inbox is the workspace chat: workspace chat and personal notes by default, direct messages, private groups, @person and #issue mentions, attachments, reactions, threads, edit/delete, typing, unread counts, live; notifications live on as Activity; chat mentions and unread DMs notify and email

## Notes

Plans: `docs/notes-plan.md`, `docs/notes-collab-plan.md`.

- [x] Notes page (replaces the private Notes chat; old notes moved over): card grid with pinned and others, title, rich text with checklists, color, per-person pin, archive and labels, search and label filters, full-screen popup kept in the URL, autosave with last saved time
- [x] Sharing: same live note for everyone on it (Yjs, named carets), "Can edit" / "Can view" per person (owner changes roles), "From <owner>" on shared cards, avatars of everyone on the note
- [x] Select notes for bulk archive and delete

## Editor and pages

- [x] Editor: link and table tools, list buttons toggle and convert lists in place, tools that don't fit go in a "More formatting" menu
- [x] Sticky page headers on every workspace page (border once the page scrolls under them)
- [x] Project files (docs/project-files-plan.md): a Files tab per project with folders, upload (drag and drop), rename, move, delete; images and videos only; the project's issue attachments listed (delete only); issue files under the description (thumbnails, "+N more" dialog); a workspace Files page; storage used in Settings → Workspace; file links follow project visibility
- [x] Direct uploads and file CDN (docs/cdn-direct-uploads-plan.md): the user app PUTs files straight to the bucket with presigned URLs (the API checks size and type after); files served by the `cdn/` Cloudflare Worker at `files.taskwell.dev` from the edge cache on signed, day-stable links when `files_cdn_url` / `files_cdn_secret` are set
- [x] Due-date reminders (docs/due-reminders-plan.md): the assignee gets "due tomorrow" the day before and "overdue" the day after, at the digest hour in their timezone; in the Taskwell bot and bell, emailed unless "Due dates" is off
- [x] Weekly digest email (docs/weekly-digest-plan.md): Mondays at the digest hour in the user's timezone, closed last week / due / blocked / blocking others across workspaces; "Weekly summary" in Settings → Notifications, on by default
- [x] Light mode (docs/light-mode-plan.md): System / Light / Dark in Settings → Profile, per browser; landing page stays dark
- [x] Sticky issue toolbar (view, group, sort, search, filters, chips) under the page header on issue lists
- [x] "Assigned by" on issues (last assignee change, else the creator): list rows, board cards, issue page
- [x] Search box on issue lists (identifier, title, description; in the URL) and on Projects; navbar "Search issues and projects" opens the palette
- [x] Two-factor setup shows a QR code

## Testing

Plan: `docs/ai-testing-plan.md`.

- [x] Phase A: test cases saved on issues (add, edit, reorder, delete), manual test runs (pass / fail / skip with a note), test history, "Create bug" from a failed result
- [x] Phase B: AI test runs: Claude writes cases and drives a sandboxed Chrome against registered test sites, live screenshots, monthly spend limit, sealed test-account passwords

## Cycles

Plan: `docs/cycles-plan.md`.

- [x] Cycles per project: numbered, start/end dates, no overlaps; current/upcoming/past from today's date; `cycles.manage` permission
- [x] Cycles page and cycle page (list/board, live progress), cycle on issues (picker, New issue preset, activity), `cycle` filter, move a past cycle's open issues to another cycle, "Go to current cycle" in the palette

## PR linking

Plan: `docs/pr-linking-plan.md`.

- [x] GitHub / GitLab webhooks (Settings → Integrations, `integrations.manage`): PRs mentioning an issue ID link to it; opening and merging move the issue to the chosen statuses; PR list on the issue page; "Copy branch name" (`⌘⇧.`)

## MCP server

Plan: `docs/mcp-plan.md`.

- [x] Personal API tokens (Settings → API tokens): create (shown once, with the `claude mcp add` command), list with last used, revoke; `Authorization: Bearer tth_…` signs any `/api` request in as that user, except auth, password and token endpoints
- [x] MCP server (`backend/mcp`, `/mcp`, streamable HTTP, stateless): list workspaces / projects / options, search, list (filters), my issues, get, create, update, comment; names instead of ids; forwards the caller's token to the API
- [x] MCP tools for every feature: AI testing (test cases, AI runs with wait, manual runs marked by the client, sites, accounts, usage), projects, cycles, issue links/watch/delete, comments, activity, labels, statuses, chat, inbox, invites
- [x] MCP tools for the rest (169 in all): project files, folders and issue attachments (base64 upload); project privacy, dates, members, milestones, updates with health, roadmap, stats and analytics, trash; notes, saved views, templates, customer feedback; workspace settings, member roles, invites, roles, statuses and status types, labels, webhooks, GitHub/GitLab integrations, CSV and GitHub import, profile and notification settings. Not exposed: token creation, MFA, password, email, sign-in and workspace deletion (API tokens are app-only)
- [x] Tests view (MCP Apps) for `get_test_run` and `list_test_cases` in Claude Desktop / claude.ai: results, notes, reproduction steps, screenshots on demand, open in Taskwell
- [x] Recorded tests (`docs/recorded-tests-plan.md`): MCP clients save browser steps (and their playwright-cli Playwright code) on test cases; "Run recorded tests" replays them in the sandboxed Chrome without Claude or the AI limit; `assert_text` / `assert_no_text` steps; "Download Playwright" exports `<ID>.spec.ts` for developers
- [x] MCP sign-in with OAuth (`docs/mcp-oauth-plan.md`): `claude mcp add --transport http taskwell <app>/mcp`, the browser asks to allow, the client gets a personal API token (listed and revocable in Settings → API tokens); Home shows the MCP URL and command
- [x] Test accounts tied to a test site (Settings → AI testing, MCP `save_test_account` `site`): the password is typed only on that site's host

## Admin app (alongside, as each phase needs it)

- [x] System config editor for `system_config` (SMTP, token lifetimes; secrets masked)
- [x] Workspaces and users list, suspend a user
- [x] Basic usage stats
- [x] Admin sign-in with separate admin accounts (`admins` table; create with `deploy/create-admin.sh email`)
- [x] Admins page: list admins (two-step status, last active), add one (shows their setup key and recovery codes once) or remove one, both confirmed with your password; logged in the audit log

## Later

- Slack
- [x] Roadmap view: project start/target dates, milestones, timeline with cycles (docs/roadmap-plan.md)
- [x] Customer feedback intake (docs/feedback-plan.md): public form link per workspace, Feedback inbox (new / linked / archived), link to or create an issue, customer feedback on the issue page, `feedback.manage`
- [x] Public API and webhooks (docs/webhooks-plan.md): API reference at `/docs/api`; signed outbound webhooks for issue.created / issue.updated / issue.deleted / comment.created with retries, delivery log and redeliver (Settings → Webhooks, `integrations.manage`)
- [x] Billing (docs/billing-plan.md): plans per account through Paddle (merchant of record): Starter free (1 workspace, 1 project, 5 members, 2 GB per workspace), Pro $12 · ₹699 (2 workspaces, 2 projects each, 10 members, 100 GB), Ultimate $49 · ₹2,499 unlimited; a workspace gets its creator's plan; INR by billing country (India); overlay checkout, in-app plan switch, Paddle portal; signed webhook linked through our checkout's transaction; admins comp plans on users; pricing cards and FAQ on the landing page
- [x] Import (docs/import-plan.md): CSV from Linear, Jira or any CSV with a Title column, and GitHub repositories ("Import issues" on the projects list and project pages); preview first, then one transaction; statuses, priorities, labels, assignees by email or name, comments; no notifications
- [x] Bulk actions (docs/bulk-actions-plan.md): checkboxes on list rows, Shift-click ranges, `X` / `Esc`; change status, assignee, priority, cycle, add or remove labels, or delete in one all-or-nothing request
- [x] Project updates (docs/project-updates-plan.md): written updates with a health (on track, at risk, off track) on the project page; health on the project, projects list, roadmap and Home; in the activity feed
- [x] Issue templates and recurring issues (docs/templates-plan.md): Settings → Templates prefill quick-create; a template can repeat every N days, weeks or months and creates the issue on schedule
- [x] Two-factor sign-in (docs/two-factor-plan.md): authenticator app plus 10 recovery codes (Settings → Profile), asked after password, email code and Google; TOTP code shared with the admin app (`backend/shared/totp`)
- [x] Trash and undo (docs/trash-plan.md): deleted issues stay 30 days (`issues` is a view over `issues_all`), Undo toast, Trash page with restore, delete forever and empty
- [x] Guests (docs/guests-plan.md): a Guest role that sees only the projects it's added to; no chat, feedback, roadmap, team or workspace settings; invite with projects
- [x] Saved views (docs/saved-views-plan.md): save a page's filters and display options by name, private or shared with the workspace, listed in the sidebar; rename, share, delete
- [x] Workspace export (docs/workspace-export-plan.md): Settings → Workspace → Export data downloads a zip of workspace.json, issues.csv (re-importable), comments.csv and the visible projects' files over a 10-minute signed link; `workspace.export`, 3 per hour
- [x] Workspace audit log (docs/workspace-audit-plan.md): Settings → Audit log, `audit.view`; members, invites, roles, workspace name/URL, projects and their members, webhooks, integrations, trash and exports, with actor and IP; private projects' entries only for who can see them; kept a year
- [x] Compliance, DPDP / GDPR / SOC 2 (docs/compliance-plan.md, docs/compliance/): self-serve account deletion with an email code (blocked while the user is the only admin of a shared workspace or has an active plan; shared content stays as "Deleted user"), Download my data (JSON), Security activity (sign-ins, failures, password/email/MFA/token changes, kept a year), new-API-token email, 18+ and terms checkbox at sign-up (`terms_accepted_at`), append-only workspace audit log, retention for old notifications and closed browser sessions, draft /privacy, /terms and /subprocessors pages (placeholders and lawyer review pending), API security headers, off-server backup option, breach and restore runbooks
- [x] Feature flags (docs/feature-flags-plan.md): admin app → Features turns chat, notes, files, roadmap, feedback, team analytics, cycles, AI testing, GitHub/GitLab, webhooks, templates, API and MCP, and imports on or off for everyone, and while on chooses which plans get each (a workspace has its owner's plan; `feature_<name>` and `feature_<name>_plans` in `system_config`, all on for every plan by default); an off feature is hidden in the user app, its pages show Not found, its API answers 404, and its background work (webhook deliveries, AI test runs, recurring templates) waits
- [x] Plan grace (docs/plan-grace-plan.md): admin Set plan comps a plan for 7, 14, 30 or 90 days or with no end; when it ends the account goes back to its paid plan or Free, its workspaces log the change and the user gets an email; "comped until" shows in the admin users list and on the Billing page. Admin audit log pages by number
- [x] Admin overview (docs/admin-overview-plan.md): totals, billing (paying, trialing, past due, cancelling, renewing, monthly/yearly, comped, accounts by plan), needs attention (comps ending, recent subscription changes), 30-day activity charts, recent signups and top workspaces, system health (mail, webhooks, AI runs and cost)
- [x] Revenue (docs/revenue-plan.md): Paddle webhooks store transactions, refunds/chargebacks and subscription prices; `make paddle-backfill` copies older history; admin Overview shows per currency MRR, 30-day revenue and earnings, refunds, chargebacks, all-time revenue, a 30-day chart and recent payments
- [x] Growth (docs/growth-plan.md): features a higher plan has show locked with a plan badge in the sidebar and settings and open an upgrade page; emails before a trial or comp ends and when a payment fails, a past-due banner with Update payment method, `/billing` link; Getting started checklist on Home (project, teammate, issue, GitHub), dismissible by workspace admins
- [x] Security and admin tools (docs/security-admin-plan.md): workspaces can require two-factor sign-in (members without it are blocked until they turn it on); creating an API token or deleting a workspace asks to confirm it's you (password, authenticator or emailed code); admin user page (workspaces, subscriptions, payments, plan history, sign-ins, plan/comp/suspend actions) and CSV export of users and payments
- [x] Estimates and velocity (docs/estimates-plan.md): 1/2/3/5/8 points on issues (sidebar, create form, list and board chips, activity, MCP); cycle points done, hourly snapshots and a burndown chart; velocity over the last 3 cycles

## Not building until a customer asks

Time tracking, gantt, resource planning, custom fields, permission matrices, SSO/SAML, automation builder, multiple issue types, portfolios.

## Next up

Phases 1–6, the admin app and Later (except Slack) are built. Next: Slack, or feedback request counts on issue lists ("Most requested" sort), once real users ask.

