# Taskwell feature list

Task management for SaaS teams. Build in phase order; don't skip ahead (see `CLAUDE.md` → Product scope).

Legend: `[x]` done, `[ ]` to do.

## Phase 1: Accounts and workspaces

- [x] Email + password sign-up and sign-in, email-code sign-in, forgot/reset password
- [x] JWT access token + rotating refresh token, Remember me, sign-out
- [x] Continue with Google (code done; add `google_client_id` / `google_client_secret` in `system_config` to turn it on)
- [x] Workspaces: default one created at sign-up, create dialog, settings page with sidebar, admin delete, name + URL slug (`taskwell.dev/acme`), switcher, settings (rename, change URL), admin/member role on membership
- [x] Team invites: invite by email from Settings → Members, accept link (`/invite`, email must match), resend or cancel; member list
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
- [x] Tests view (MCP Apps) for `get_test_run` and `list_test_cases` in Claude Desktop / claude.ai: results, notes, reproduction steps, screenshots on demand, open in Taskwell
- [x] Recorded tests (`docs/recorded-tests-plan.md`): MCP clients save browser steps (and their playwright-cli Playwright code) on test cases; "Run recorded tests" replays them in the sandboxed Chrome without Claude or the AI limit; `assert_text` / `assert_no_text` steps; "Download Playwright" exports `<ID>.spec.ts` for developers
- [x] MCP sign-in with OAuth (`docs/mcp-oauth-plan.md`): `claude mcp add --transport http taskwell <app>/mcp`, the browser asks to allow, the client gets a personal API token (listed and revocable in Settings → API tokens); Home shows the MCP URL and command
- [x] Test accounts tied to a test site (Settings → AI testing, MCP `save_test_account` `site`): the password is typed only on that site's host

## Admin app (alongside, as each phase needs it)

- [x] System config editor for `system_config` (SMTP, token lifetimes; secrets masked)
- [x] Workspaces and users list, suspend a user
- [x] Basic usage stats
- [x] Admin sign-in for platform admins (promote with `taskwell-admin -promote email`)

## Later

- Slack
- [x] Roadmap view: project start/target dates, milestones, timeline with cycles (docs/roadmap-plan.md)
- [x] Customer feedback intake (docs/feedback-plan.md): public form link per workspace, Feedback inbox (new / linked / archived), link to or create an issue, customer feedback on the issue page, `feedback.manage`
- [x] Public API and webhooks (docs/webhooks-plan.md): API reference at `/docs/api`; signed outbound webhooks for issue.created / issue.updated / issue.deleted / comment.created with retries, delivery log and redeliver (Settings → Webhooks, `integrations.manage`)
- Billing (Stripe, per seat)
- [x] Import (docs/import-plan.md): CSV from Linear, Jira or any CSV with a Title column, and GitHub repositories (Settings → Import); preview first, then one transaction; statuses, priorities, labels, assignees by email or name, comments; no notifications
- [x] Bulk actions (docs/bulk-actions-plan.md): checkboxes on list rows, Shift-click ranges, `X` / `Esc`; change status, assignee, priority, cycle, add or remove labels, or delete in one all-or-nothing request
- [x] Saved views (docs/saved-views-plan.md): save a page's filters and display options by name, private or shared with the workspace, listed in the sidebar; rename, share, delete

## Not building until a customer asks

Time tracking, gantt, resource planning, custom fields, permission matrices, SSO/SAML, automation builder, multiple issue types, portfolios.

## Next up

Phases 1–6 and the admin app are built. Next: pick from Later (cycles, PR linking, Slack, roadmap) once real users ask.

