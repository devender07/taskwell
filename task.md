# Tether feature list

Task management for SaaS teams. Build in phase order; don't skip ahead (see `CLAUDE.md` → Product scope).

Legend: `[x]` done, `[ ]` to do.

## Phase 1: Accounts and workspaces

- [x] Email + password sign-up and sign-in, email-code sign-in, forgot/reset password
- [x] JWT access token + rotating refresh token, Remember me, sign-out
- [x] Continue with Google (code done; add `google_client_id` / `google_client_secret` in `system_config` to turn it on)
- [x] Workspaces: default one created at sign-up, create dialog, settings page with sidebar, admin delete, name + URL slug (`tether.app/acme`), switcher, settings (rename, change URL), admin/member role on membership
- [ ] Team invites: invite by email, accept link, resend or cancel
- [ ] Roles: admin (billing, members, settings) and member
- [ ] Profile: ~~name~~ (done: Settings → Profile), avatar, change password

## Phase 2: Projects and issues

- [ ] Projects: name, key (`ENG`), description, archive
- [ ] Issues: title, markdown description, status, assignee, priority, due date, labels
- [ ] Readable issue IDs (`ENG-142`) used in URLs
- [ ] Default statuses (Backlog, Todo, In progress, Done, Cancelled), editable per workspace
- [ ] Sub-issues (one level) and "blocks / blocked by" links

## Phase 3: Views

- [ ] List view with grouping (status, assignee, priority) and sorting
- [ ] Kanban board with drag between columns
- [ ] Filters by assignee, status, label, priority, kept in the URL so they can be shared
- [ ] "My issues" view

## Phase 4: Collaboration

- [ ] Comments on issues (markdown)
- [ ] Activity log (status changes, reassignments, edits)
- [ ] `@mentions` in comments and descriptions, notified by email
- [ ] Image and file attachments in descriptions and comments

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

- [ ] System config editor for `system_config` (SMTP, token lifetimes; secrets masked)
- [ ] Workspaces and users list, suspend a user
- [ ] Basic usage stats

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
