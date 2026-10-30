# Tether feature list

Task management for SaaS teams. Build in phase order; don't skip ahead (see `CLAUDE.md` → Product scope).

Legend: `[x]` done, `[ ]` to do.

## Phase 1: Accounts and workspaces

- [x] Email + password sign-up and sign-in, email-code sign-in, forgot/reset password
- [x] JWT access token + rotating refresh token, Remember me, sign-out
- [x] Continue with Google (code done; add `google_client_id` / `google_client_secret` in `system_config` to turn it on)
- [x] Workspaces: default one created at sign-up, create dialog, settings page with sidebar, admin delete, name + URL slug (`tether.app/acme`), switcher, settings (rename, change URL), admin/member role on membership
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
- [x] Filters by assignee, status, label, priority, kept in the URL so they can be shared
- [x] "My issues" view

## Phase 4: Collaboration

- [x] Rich text descriptions (Tiptap): formatting toolbar, markdown shortcuts, live collaborative editing with named cursors (Yjs relayed and stored by the Go API, across instances via Postgres LISTEN/NOTIFY)
- [x] Comments on issues (rich text, reusing the shared editor; edit and delete; `comments.create` / `comments.manage`)
- [x] Activity log (status changes, reassignments, edits)
- [x] `@mentions` in comments and descriptions, notified by email
- [x] Image and file attachments in descriptions and comments (local disk under `UPLOAD_DIR`; paste, drop or the attach button)

## Phase 5: Keyboard-first

- [ ] Command palette (`⌘K`): jump to anything, run any action
- [ ] Quick-create issue (`C`) from anywhere
- [ ] Fast search across issues and projects
- [ ] Single-key shortcuts on an issue: `A` assign, `S` status, `P` priority, `L` label

## Phase 6: Notifications

- [ ] In-app inbox: mentions, assignments, status changes on watched issues
- [ ] Email notifications with per-user settings and a daily digest option
- [ ] Watch / unwatch an issue

## Admin app (alongside, as each phase needs it)

- [x] System config editor for `system_config` (SMTP, token lifetimes; secrets masked)
- [x] Workspaces and users list, suspend a user
- [x] Basic usage stats
- [x] Admin sign-in for platform admins (promote with `tether-admin -promote email`)

## Later

- Cycles / sprints
- GitHub / GitLab PR linking (an issue moves when its PR merges)
- Slack
- Roadmap view
- Customer feedback intake
- Public API and webhooks
- Billing (Stripe, per seat)

## Not building until a customer asks

Time tracking, gantt, resource planning, custom fields, permission matrices, SSO/SAML, automation builder, multiple issue types, portfolios.

## Next up

Workspace + invites + roles. Every later table (projects, issues, comments) belongs to a workspace and checks a role, so they come first.
