# Tether

Project management tool for SaaS companies. Early stage: no code yet, only this file and DESIGN.md.

## Design source of truth

`DESIGN.md` is the DesignMD document for the visual system (Linear-inspired, dark-first). Follow it exactly for colors, type, spacing, radii, shadows, z-index, and component specs. Do not invent tokens. Where the front matter and the body disagree, the body and section 9 "Agent Prompt Guide" win.

Quick reference:
- Background `#08090A`, nested surface `#0F1011`, text `#F7F8F8`, secondary text `#8A8F98`
- Accents only: pink `#F91880` (CTA/attention), green `#00BA7C` (success). Links `#5E6AD2`
- Inter Variable for UI, Berkeley Mono for code. Weights 400 / 510 / 590 only
- 8px grid. Spacing steps: 4, 8, 12, 16, 20, 24, 32, 40, 48, 56, 96
- Radius: pill `9999px` for buttons, `6px` inputs, `8px` cards, `0` full-width sections
- Breakpoints 640 / 1024 / 1440. Z-index 1, 2-3, 50, 100+, 995-997
- Contrast 4.5:1, touch targets 44px, line-height >= 1.4x

## Product scope

Build order, do not skip ahead:

1. Workspace, auth (magic link or Google), team invites, admin/member roles
2. Projects and issues: title, markdown description, status, assignee, priority, due date, labels
3. List and kanban views only, with filters by assignee/status/label
4. Comments and activity log on issues, mentions notify by email
5. Keyboard-first: command palette, quick-create, fast search
6. Notifications (email and in-app)

Later: cycles/sprints, GitHub/GitLab PR linking, Slack, roadmap view, customer feedback intake, public API and webhooks.

Not building until a customer asks: time tracking, gantt, resource planning, custom fields, permission matrices, SSO/SAML, automation builder, multiple issue types, portfolios.

## Working style

- Smallest thing that works. Reuse before writing, stdlib before dependency, one line before fifty.
- No speculative abstractions, no scaffolding for later.
- Non-trivial logic leaves one runnable check behind.
- Mark deliberate shortcuts with a `ponytail:` comment naming the ceiling and upgrade path.

## Repo layout

```
backend/            Go workspace (go.work)
  admin/            module github.com/devender07/tether/backend/admin, entry cmd/admin
  user/             module github.com/devender07/tether/backend/user,  entry cmd/user
frontend/           npm workspaces, no framework yet
  admin/            @tether/admin
  user/             @tether/user
```

- Go: flat layout, `cmd/<svc>/main.go` wires and runs, logic goes in `internal/`. Manual constructor injection, no DI library. Config via env vars, logs to stdout.
- Build/vet from `backend/`: `go build github.com/devender07/tether/backend/admin/... github.com/devender07/tether/backend/user/...`
- Frontend packages are empty shells. No framework or dependency chosen yet, do not add one without asking.

## Git identity

This repo uses the `devender07` GitHub account, not the tezminds one. Author is set repo-locally (`git config user.*`), and `credential.useHttpPath` is on globally so HTTPS credentials are stored per repository URL. Push with devender07's token when prompted.
