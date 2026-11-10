# Feature flags

Staff turn whole features on or off for everyone from the admin app. When a feature is off, the user app doesn't show
it anywhere: no sidebar link, no settings page, no panels, buttons or palette commands, and its URLs show "Not found".
The API refuses its endpoints, so MCP clients and API tokens can't reach it either. Data is kept: turning the feature
back on brings everything back as it was.

Per plan: each feature is on for some plans (Free, Starter, Pro, Advanced) and off for the rest. A workspace gets the
features of its owner's plan (`plan.ForWorkspace`, the same rule as the limits), so everyone in a Pro owner's
workspace has the Pro features whatever their own plan. Off for a plan means hidden, the same as off for everyone
(no upgrade prompts in this version).

## Flags

Two `system_config` rows per feature, so switching a feature off and on again keeps its plans:
- `feature_<name>`: `true` or `false`, the switch for everyone. `false` turns it off on every plan.
- `feature_<name>_plans`: the comma-separated plan ids that get it while it's on (`free,starter,pro,advanced`),
  empty for none; order doesn't matter, unknown ids are ignored.
A missing or unreadable row counts as on / every plan. Seeded on for every plan (nothing changes on deploy). Not
secret. Descriptions are what the admin Features page shows.

| Key | Label | What it covers |
|---|---|---|
| `feature_chat` | Chat | Inbox page (workspace chat, direct messages, groups) |
| `feature_notes` | Notes | Notes page, shared notes |
| `feature_files` | Files | Workspace Files page, project Files tab and folders |
| `feature_roadmap` | Roadmap | Roadmap page, project milestones |
| `feature_feedback` | Customer feedback | Feedback page, public feedback form, Settings → Feedback form |
| `feature_team` | Team analytics | Team page, member analytics |
| `feature_cycles` | Cycles | Cycles pages, cycle on issues (picker, filter, grouping) |
| `feature_ai_testing` | AI testing | Test cases and runs on issues, Settings → AI testing, test sites and accounts |
| `feature_integrations` | GitHub and GitLab | Settings → Integrations, pull requests on issues, inbound PR webhooks |
| `feature_webhooks` | Webhooks | Settings → Webhooks, outgoing deliveries |
| `feature_templates` | Templates | Settings → Templates, template picker in New issue |
| `feature_api` | API and MCP | Settings → API tokens, token auth, MCP OAuth |
| `feature_imports` | Imports | CSV and GitHub import |

Core (issues, projects, views, comments, members, roles, billing, notifications, trash, audit) can't be turned off.

## Schema

`backend/user/internal/db/migrations/035_feature_flags.sql`: insert the 13 rows above
(`feature_<name>` = `true`, `feature_<name>_plans` = `free,starter,pro,advanced`), `on conflict do nothing`. (035
isn't deployed yet, so it's edited in place; the dev and test databases were converted by hand.)

## API

### User API

- `GET /api/workspaces/{slug}/features`, for a member (404 otherwise, like the other workspace routes):
  `200 {"chat": true, "notes": false, ...}`, every flag by name (key without `feature_`) for that workspace's plan.
- `GET /api/features`, public: every flag by name with the plan ids it's on for (`{"chat": ["free","starter","pro","advanced"], "ai_testing": []}`), for the pricing page.
- Flags are read through `sysconfig` (cached; admin saves invalidate it already). A missing or unreadable row counts
  as on for every plan.
- Every endpoint of a feature that's off for the workspace's plan answers `404 {"error": "This feature is turned
  off."}`. The workspace comes from the path's `{slug}`, the public feedback form's token, or the inbound git hook's
  token. Endpoints with no workspace (`/api/me/tokens`, `/api/oauth/`) are off only when the feature is off for
  every plan.
- API tokens: a Bearer request to a workspace route answers the 404 when `api` is off for that workspace's plan; a
  Bearer request outside a workspace when it's off for every plan. Cookie sessions are unaffected.
- Background work pauses per workspace: outgoing webhook deliveries stay queued (not dropped) and go out once the
  plan has the feature again; AI test runs aren't started; recurring templates don't create issues.
- One package (`internal/feature`) owns the names, `Enabled(ctx, name) bool`, the route → feature table and the
  middleware; a test checks a disabled feature's route answers 404 and an enabled one passes.

### Admin API

- No new endpoints: the admin app reads `GET /api/config` and saves with `POST /api/config/{key}` (FormData `value`).
- `checkValue`: `feature_<name>` accepts `true` or `false` ("Use true or false."); `feature_<name>_plans` a
  comma-separated list of known plan ids, or empty ("Use plan ids: free, starter, pro, advanced.").

## Frontend

### Admin app

- Page `/features` (nav item "Features", after Config): a table with one row per feature (its label), an "On" checkbox
  (saves `feature_<name>` = `true`/`false`) and one checkbox per plan (Free, Starter, Pro, Advanced; saves
  `feature_<name>_plans`). While a feature is off its plan checkboxes are dimmed and disabled but keep their values.
  Each save is `POST /api/config/{key}`, rolled back with an error on failure.
- The Config page leaves `feature_*` keys out.

### User app

- The `$workspace` route fetches `/workspaces/{slug}/features` (in `beforeLoad`, so child guards can read it from
  context) instead of `_app` fetching `/features`; `useFeature(name)` reads the current workspace's flags. Switching
  workspace switches the flags.
- Off features are hidden from: sidebar, settings nav (and its bottom sheet), command palette, home blocks, issue page
  panels (test cases, pull requests, cycle), issue filters and grouping (cycle), project tabs (files, cycles,
  milestones), New issue (template, cycle), import entry points.
- Their routes' `beforeLoad` throws `notFound()`, so a bookmarked URL shows the shared NotFound.
- Public feedback form (`/f/$token`): shows "This form isn't available." when off.

## Not doing

- Live hiding in open tabs: a change shows on the next page load or navigation that reloads `_app`.
  `ponytail: no realtime push of flag changes; broadcast a features event if staff need instant effect.`
- Per-workspace overrides, scheduled rollouts, percentages.
- Upgrade prompts for plan-gated features. (The pricing page lists them: `GET /api/features` returns each feature's plan ids; a card shows the features only some paid plans have, "Every plan includes" the ones all paid plans have.)
