# Tether

Project management tool for SaaS companies. Early stage: no code yet, only this file and DESIGN.md.

## Design source of truth

`DESIGN.md` is the DesignMD document for the visual system (Linear-inspired, dark-first). Follow it exactly for colors, type, spacing, radii, shadows, z-index, and component specs. Do not invent tokens. Where the front matter and the body disagree, the body and section 9 "Agent Prompt Guide" win.

Quick reference:
- Background `#08090A`, nested surface `#0F1011`, text `#F7F8F8`, secondary text `#8A8F98`
- Accents only: pink `#F91880` (CTA/attention), green `#00BA7C` (success). Links `#5E6AD2`
- Plus Jakarta Sans for UI, Berkeley Mono for code. Weights 400 / 510 / 590 only
- 8px grid. Spacing steps: 4, 8, 12, 16, 20, 24, 32, 40, 48, 56, 96
- Radius: pill `9999px` for buttons, `6px` inputs, `8px` cards, `0` full-width sections
- Breakpoints 640 / 1024 / 1440. Z-index 1, 2-3, 50, 100+, 995-997
- Contrast 4.5:1, touch targets 44px, line-height >= 1.4x
- Sentence case for all UI text and user-facing messages: headings, buttons, labels, tabs, menus, placeholders, page titles, errors and email subjects ("Create account", "Forgot password?", not "Create Account"). Only proper nouns keep capitals (Tether, Google).

## Product scope

Build order, do not skip ahead:

1. Workspace, auth (magic link or Google), team invites, custom roles with permissions
2. Projects and issues: title, rich text description, status, assignee, priority, due date, labels
3. List and kanban views only, with filters by assignee/status/label
4. Comments and activity log on issues, mentions notify by email, live collaborative editing of descriptions
5. Keyboard-first: command palette, quick-create, fast search
6. Notifications (email and in-app)

Later: cycles/sprints, GitHub/GitLab PR linking, Slack, roadmap view, customer feedback intake, public API and webhooks.

Not building until a customer asks: time tracking, gantt, resource planning, custom fields, SSO/SAML, automation builder, multiple issue types, portfolios.

## Working style

- Smallest thing that works. Reuse before writing, stdlib before dependency, one line before fifty.
- No speculative abstractions, no scaffolding for later.
- Non-trivial logic leaves one runnable check behind.
- Mark deliberate shortcuts with a `ponytail:` comment naming the ceiling and upgrade path.
- do not add unnecessary comments in components

## Skills

Load the matching skill before writing or reviewing code. Project skills live in `.claude/skills/`.

- Go: `golang-*` matching the task (project-layout, code-style, naming, error-handling, structs-interfaces, context, concurrency, database, security, testing)
- React/frontend: `react-best-practices`
- Reviews: `code-review` for bugs, `simplify` / `ponytail-review` for over-engineering, `security-review` before merging auth or data changes

## Feature workflow

Every new feature from `task.md` goes through these steps:

1. **Plan first.** Write `docs/<feature>-plan.md` before any code: scope, schema/migrations, `system_config` keys, the API contract (method, path, FormData fields, JSON responses, error codes), and the frontend routes and forms. The contract is fixed once agents start.
2. **Split into two agents, run in parallel**, each briefed with the plan path, this file, and its scope:
   - **Backend agent**: only `backend/` and `docker-compose.yml`. Reads the `golang-*` skills in `.claude/skills/` that fit the work (always `golang-database`, `golang-security`, `golang-error-handling`; add others as needed). Follows the routes/controller/model layout. Verifies with `go vet`, `go test` and curl against the running API.
   - **Frontend agent**: only `frontend/`. Reads `.claude/skills/react-best-practices/SKILL.md`. Uses TanStack Router for loading and guards, `src/api.ts` for calls, shared components and validators. Verifies with `npm run build`.
3. **Integrate in the main session:** run both against each other, click through every flow end to end, fix gaps, update `task.md`, then commit.

Small fixes and follow-ups to a shipped feature don't need a plan or agents.

## Repo layout

```
backend/            Go workspace (go.work)
  admin/            module github.com/devender07/tether/backend/admin, entry cmd/admin
  user/             module github.com/devender07/tether/backend/user,  entry cmd/user
frontend/           npm workspaces (shared, admin, user)
  shared/           @tether/shared: src/styles/tokens.css (DESIGN.md), src/components/<name>/<name>.tsx (one folder per component, css alongside)
  admin/            @tether/admin: Vite + React 19 + TanStack Router, port 5174
  user/             @tether/user:  Vite + React 19 + TanStack Router, port 5173
```

- Go: flat layout, `cmd/<svc>/main.go` wires and runs, logic goes in `internal/`. Manual constructor injection, no DI library. Config via env vars, logs to stdout.
- Go feature packages (`internal/<feature>/`) split by role: `routes.go` mounts endpoints, `controller.go` holds HTTP handlers (parse, validate, respond, no SQL), `model.go` holds types and every SQL query. Helpers get their own file (e.g. `session.go`, `validate.go`).
- Run the whole backend from `backend/`: `make` (Postgres, Mailpit, Chrome, then user API, AI tester, admin API and MCP server; Ctrl+C stops all). `make build|vet|test|down`.
- Build/vet from `backend/`: `go build github.com/devender07/tether/backend/admin/... github.com/devender07/tether/backend/user/...`
- Frontend stack is fixed: Vite, React 19, TanStack Router with file-based routes. No UI kit, no CSS framework, no state library, no schema library. Ask before adding any dependency.
- Imports: `@/` is the app's `src/` (both apps). Use it instead of `../`; same-folder `./` imports stay relative.
- Styling: utility classes from `@tether/shared/utilities.css` first, inline `style` for one-off values, a CSS file only for what neither can express (`:hover`, `:focus`, `[aria-*]`, pseudo-elements, media queries). Both apps import `tokens.css` and `utilities.css` once in `main.tsx`; shared components rely on that.
- Frontend commands from `frontend/`: `npm run dev:user`, `npm run dev:admin`, `npm run build` (vite build + tsc per app).

## TanStack Router conventions

- Routes live in `src/routes/`. `src/routes/` holds route files only; related pages share a `(group)` folder that doesn't change the URL (e.g. `routes/(auth)/signin.tsx` is `/signin`), and their parts (forms, CSS) go in `src/components/<group>/`. `src/routeTree.gen.ts` is generated by the Vite plugin and committed. Never edit it.
- Router defaults in `src/router.tsx`: intent preloading, automatic code splitting, shared Pending / ErrorFallback / NotFound from `@tether/shared/boundaries`.
- Shareable UI state (filters, view, sort, page) goes in the URL via `validateSearch` on the route that owns it.
- Forms use `noValidate`; no browser validation attributes (`required`, `pattern`, `min`). Validate in the form action with `@tether/shared/validate` and pass messages to `<Input error>`.
- Links using a `search={(prev) => ...}` reducer pass `from={Route.fullPath}` so `prev` is typed.

## Git identity

This repo uses the `devender07` GitHub account, not the tezminds one. Author is set repo-locally (`git config user.*`), and `credential.useHttpPath` is on globally so HTTPS credentials are stored per repository URL. Push with devender07's token when prompted.
