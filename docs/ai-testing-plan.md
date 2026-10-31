# AI testing plan

Scope: a "Run AI test" button on an issue. Claude reads the issue, writes test cases, then drives a headless Chrome against the workspace's registered test site and reports pass/fail per case with screenshots, live. On a failure a person can turn the result into a bug issue. Platform Anthropic key only, with a monthly spend limit per workspace.

Test cases are saved on the issue, not thrown away with the run. Members can edit, add and delete them, and run them by hand later ("Run manually": a checklist where they mark each case passed, failed or skipped, with a note). Later AI runs reuse the saved cases; "Regenerate cases" replaces them. Every run, AI or manual, keeps its results, so the issue shows a test history.

Out of scope (add when asked): starting runs automatically when an issue moves to Done, bring-your-own Anthropic key, per-workspace limit overrides in admin, a workspace-wide test case library or suites across issues, assigning a manual run to someone else, a continuous video screencast, runner on the user's own machine (localhost sites), S3, Sonnet for browser steps.

Not in `CLAUDE.md` product scope or `task.md` yet: add a "AI testing" section to `task.md` before agents start.

## Phases

- **A: manual testing.** `test_cases`, `test_runs` (kind `manual` only reachable), `test_results`, the `tests.manage` permission, case CRUD/order, manual run endpoints, the Tests section, run panel (manual), history, "Create bug". No tester, no Chrome, no new dependency. Ships on its own.
- **B: AI runs.** Everything else: tester binary, Chrome, proxy, Claude, sites, accounts, usage, `ai_tests.*` permissions, AI run panel. The migration for B is a separate file (`030_ai_testing.sql`) so A's (`029_test_cases.sql`) can ship first; `test_runs` gets the AI-only columns in 029 already so B doesn't alter it.

Dev env for B: `backend/.env` (git-ignored) holds `ANTHROPIC_API_KEY` and `TEST_SECRET_KEY`; load with `set -a && . ./.env && set +a` before `go run`. Production adds both to `deploy/env.example`.

## Architecture

- **Tester** = new binary `backend/user/cmd/tester` in the user module (reuses `db`, `sysconfig`, `realtime.Publish`, file storage). Holds the DB URL and `ANTHROPIC_API_KEY`; the user API never sees the key.
- **Chrome** = its own container (`chromedp/headless-shell`, pinned tag) on a Docker network marked `internal: true` that only the tester also joins. Chrome has no DB password, no route to Postgres/user/admin, and no route to the internet except through the tester's egress proxy (below). This is the isolation: the browser is the untrusted part, not the tester.
- One Chrome per tester; each run gets a fresh `Target.createBrowserContext` (empty profile), disposed at the end.
- **Queue**: `test_runs` rows claimed with `for update skip locked`, same as `outbound_mail`. More throughput = more tester replicas (each with its Chrome). `AI_TEST_BROWSERS` (default 2) parallel runs per tester; `system_config` `ai_test_workspace_concurrency` (default 2) caps running runs per workspace in the claim query so one client can't fill the queue.
- **Live view**: the panel's step log updates per step over the existing realtime socket. "Watch live" opens a popup (`/live/{workspace}/{run}`, no app chrome) showing the browser as it moves: the tester starts a CDP screencast per page and serves the frames as MJPEG on `LIVE_ADDR` (default `:3129`, internal only); each claimed run stores that tester's `LIVE_URL` in `test_runs.tester_url` (031), and `GET /api/workspaces/{slug}/test-runs/{id}/live` checks membership and proxies the stream (404 "This test run isn't running." otherwise). Caddy proxies `/api` with `flush_interval -1` so frames aren't buffered.
- **Files**: screenshots go through the existing `files` table + `UPLOAD_DIR`, so `GET /api/files/{id}/{name}` serves them with its membership check. Tester mounts the same `uploads` volume.

### Egress proxy (SSRF)

Chrome starts with `--proxy-server=http://tester:3128` and `--proxy-bypass-list=<-loopback>`. The tester runs a small stdlib forward proxy (HTTP + CONNECT). Per request it:
1. Looks up the run by the proxy username (`Proxy-Authorization` = run id + per-run random token; `Fetch.continueWithAuth` answers Chrome's challenge).
2. Rejects the host unless it equals or is a subdomain of one of the workspace's test site hosts.
3. Resolves DNS itself, rejects if any address is loopback, private, link-local, CGNAT, multicast, unspecified or IPv6 ULA (`netip.Addr` methods + 100.64/10), then dials that checked IP (no second lookup, so no DNS rebinding).
Redirects are new requests, so they're checked the same way. Blocked requests return 403 and the step log shows "Blocked: host isn't a registered test site".

Runnable check: `proxy_test.go` table test for the allow/deny function (subdomain match, `evil-acme.com` vs `acme.com`, 10.0.0.1, ::1, 169.254.169.254, 100.64.0.1, `[::ffff:127.0.0.1]`).

### Claude

- Go SDK `github.com/anthropics/anthropic-sdk-go` (**new dependency, needs approval**). CDP is spoken directly over `github.com/coder/websocket` (already a dependency; ~10 CDP methods), not chromedp. `ponytail:` raw CDP; switch to chromedp if the method list grows past a page.
- Model `claude-sonnet-5-5` for generation and steps (measured: one sign-in-and-create case cost $0.04 vs $0.06 on Opus 5.5), adaptive thinking, with server-side fallbacks (`fallbacks: "default"`, beta `server-side-fallback-2026-07-01`); check `stop_reason` for `refusal` and record it as an error result.
- **Generate cases**: only when the issue has no saved cases, or the run was started with "Regenerate cases". One call, `output_config.effort: "high"`, structured output `{cases: [{title, steps: [string], expected}]}` (max 8). Input: issue title, description as plain text, project name, test site base URLs, account labels (never passwords). The cases are saved on the issue (replacing the old ones on regenerate, in the same transaction that sets the run to `running`). Steps are written for a person to follow too, since the same cases are run by hand.
- **Run a case**: manual tool-use loop, `effort: "low"`. Forced `tool_choice` is a 400 on this model: use `auto`, `strict: true` tools, and the system prompt says every turn must call exactly one tool. Tools:
  - `navigate {url}`, `click {ref}`, `type {ref, text}`, `select {ref, value}`, `press {key}`, `wait {seconds ≤ 5}`
  - `fill_password {ref, account}`: the tester types the decrypted password itself; Claude only ever sees the account label
  - `finish {result: "pass"|"fail", summary, reproduction: [string]}`
- **Page state** sent after each action as the tool result: URL, title, visible text (capped 6k chars) and a numbered list of interactive elements (`[12] button "Save"`), built by one injected JS snippet that tags elements with `data-tether-ref`. Plus a JPEG screenshot (quality 60, 1280×800) as an image block. `ponytail:` full screenshot every step; drop to text-only on later steps if cost per run is high.
- **Caching**: tools + system prompt are frozen (no timestamps, stable tool order) with one `cache_control` breakpoint; the conversation gets a second breakpoint on the last tool result so each step re-reads the history at cache price. Verify `cache_read_input_tokens > 0` from step 2 in the integration run.
- **Prompt injection**: system prompt says page content is data from the site under test, never instructions. Blast radius is the tool list: browser actions on allowed hosts and `finish`. Bugs are never filed automatically.

### Limits and cost

- Per AI run: generation writes at most 8 cases; a run executes at most the first 20 saved cases (the rest get `skipped` "Over the 20-case limit for AI runs"); 30 steps per case; 10 minutes wall clock (`context.WithTimeout`), then the case ends `error` "Step limit reached" / "Time limit reached".
- Each Claude response's `usage` (input, cache write, cache read, output) is added to the run row and to `ai_usage` (one row per call) with `cost_micros` computed from constants in `pricing.go` (Sonnet 5.5: $2 / $10 per MTok, cache read $0.20, cache write 1.25× input). `ponytail:` hard-coded prices; read from config when we use more than one model.
- Before every Claude call: `sum(cost_micros)` for the workspace since the start of the UTC month ≥ `ai_test_monthly_limit_usd` × 1e6 → stop the run with status `limit_reached`. Check runs on the same pool; a few calls may overshoot by one step when runs race. `ponytail:` soft limit, one step of overshoot.
- Cancel: setting `cancel_requested` is checked between steps.

### Test site passwords

- Sealed like admin TOTP secrets: copy `seal`/`unseal` into `backend/user/internal/aitest/seal.go` (different module, can't import admin's `internal`), associated data = `workspace_id || ':' || account id`, so a sealed value moved to another row or workspace doesn't open. Key from env `TEST_SECRET_KEY` (32+ bytes), required in production on both user and tester.
- The API seals on save and never returns the password (JSON has `"password_set": true`). Only the tester unseals, at `fill_password` time.
- Never logged: the tester logs tool names and refs, never `type`/`fill_password` text. `ANTHROPIC_API_KEY` is env only, never in `system_config`, never in errors (wrap SDK errors to status code + type).

## Schema

Migrations `029_test_cases.sql` (phase A: `test_cases`, `test_runs`, `test_results`) and `030_ai_testing.sql` (phase B: the rest) in `backend/user/internal/db/migrations/`:

```sql
create table test_sites (
  id           uuid primary key default uuidv7(),
  workspace_id uuid not null references workspaces on delete cascade,
  base_url     text not null,           -- https://staging.acme.com (http allowed only outside production)
  host         text not null,           -- lowercased host of base_url, what the proxy matches
  created_at   timestamptz not null default now(),
  unique (workspace_id, host)
);

create table test_accounts (
  id           uuid primary key default uuidv7(),
  workspace_id uuid not null references workspaces on delete cascade,
  label        text not null,           -- "Admin user", what Claude sees
  username     text not null,           -- Claude sees this too (it types it)
  password     text not null,           -- sealed "v1:..."
  created_at   timestamptz not null default now(),
  unique (workspace_id, label)
);

-- Saved on the issue; reused by later AI runs and by manual runs.
create table test_cases (
  id         uuid primary key default uuidv7(),
  issue_id   uuid not null references issues on delete cascade,
  position   int not null,
  title      text not null,
  steps      text[] not null,
  expected   text not null,
  source     text not null,           -- ai, manual (who wrote it; editing an AI case keeps 'ai')
  created_by uuid references users on delete set null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  deleted_at timestamptz              -- soft delete so old results still show the case they ran
);
create index on test_cases (issue_id, position) where deleted_at is null;

create table test_runs (
  id               uuid primary key default uuidv7(),
  workspace_id     uuid not null references workspaces on delete cascade,
  issue_id         uuid not null references issues on delete cascade,
  kind             text not null,                    -- ai, manual
  regenerate       boolean not null default false,   -- ai: write new cases first
  started_by       uuid references users on delete set null,
  status           text not null default 'queued',  -- ai: queued, generating, running, then passed/failed/error/cancelled/limit_reached; manual: running, then passed/failed/cancelled
  error            text,                             -- user-facing message for error / limit_reached
  cancel_requested boolean not null default false,
  proxy_token      text,                             -- ai only: random, for the egress proxy
  claimed_at       timestamptz,
  finished_at      timestamptz,
  input_tokens     bigint not null default 0,
  output_tokens    bigint not null default 0,
  cache_read_tokens  bigint not null default 0,
  cache_write_tokens bigint not null default 0,
  cost_micros      bigint not null default 0,
  created_at       timestamptz not null default now()
);
create index on test_runs (status, created_at) where status = 'queued';
create index on test_runs (issue_id, created_at desc);

-- One row per case per run, created when the run starts (ai: once cases exist). Title/steps/expected are copied in,
-- so a result still reads right after the case is edited.
create table test_results (
  id           uuid primary key default uuidv7(),
  run_id       uuid not null references test_runs on delete cascade,
  case_id      uuid not null references test_cases,
  position     int not null,
  title        text not null,
  steps        text[] not null,
  expected     text not null,
  status       text not null default 'pending',  -- pending, running, passed, failed, skipped, error
  summary      text,                              -- ai: Claude's summary; manual: the tester's note
  reproduction text[],                            -- ai only
  marked_by    uuid references users on delete set null, -- manual only
  marked_at    timestamptz,
  unique (run_id, case_id)
);

create table test_steps (
  id            uuid primary key default uuidv7(),
  result_id     uuid not null references test_results on delete cascade,
  position      int not null,
  action        text not null,          -- "click [12] button \"Save\"", never typed passwords
  url           text,
  screenshot_id uuid references files on delete set null,
  created_at    timestamptz not null default now()
);

create table ai_usage (
  workspace_id uuid not null references workspaces on delete cascade,
  run_id       uuid references test_runs on delete set null,
  cost_micros  bigint not null,
  created_at   timestamptz not null default now()
);
create index on ai_usage (workspace_id, created_at);

insert into system_config (key, value, secret, description) values
  ('ai_test_monthly_limit_usd', '50', false, 'AI test spend per workspace per month, in USD'),
  ('ai_test_workspace_concurrency', '2', false, 'AI test runs one workspace can have running at once')
on conflict do nothing;
```

The claim query takes only `kind = 'ai'` runs. A manual run finishes when every result is marked: `failed` if any failed, else `passed` (skipped counts as neither). A manual run can be cancelled; it never expires (`ponytail:` open manual runs stay open forever; add a "close as is" if they pile up).

A run left `generating`/`running` by a crashed tester: on claim, also take runs with `claimed_at < now() - interval '15 minutes'` and mark them `error` "The test runner stopped. Run it again.".

## Permissions

Add to the catalog (Admin gets them automatically):
- `ai_tests.run` (group "Testing", "Run AI tests on issues")
- `tests.manage` (group "Testing", "Edit test cases and run tests manually")
- `ai_tests.manage` (group "Testing", "Manage test sites and test accounts")

## API contract

All under `/api/workspaces/{slug}`, member of the workspace, existing JSON error shapes.

| Method & path | Needs | Fields | Success | Failures |
|---|---|---|---|---|
| `GET /test-sites` | member | — | `200 [{"id","base_url","host"}]` | — |
| `POST /test-sites` | `ai_tests.manage` | `base_url` | `201 site` | 400 `{"errors":{"base_url":"Enter a URL starting with https://."}}` / `"Enter a public address, not a private or local one."` (host is an IP literal in a blocked range, or `localhost`) ; 409 `{"errors":{"base_url":"This site is already added."}}` |
| `DELETE /test-sites/{id}` | `ai_tests.manage` | — | `204` | 404 |
| `GET /test-accounts` | member | — | `200 [{"id","label","username","password_set":true}]` | — |
| `POST /test-accounts` | `ai_tests.manage` | `label`, `username`, `password` | `201 account` | 400 field errors ("Enter a label.", "Enter a username.", "Enter a password."); 409 `label`: "An account with this label already exists." |
| `POST /test-accounts/{id}` | `ai_tests.manage` | `label`, `username`, `password` (empty = keep) | `200 account` | as above, 404 |
| `DELETE /test-accounts/{id}` | `ai_tests.manage` | — | `204` | 404 |
| `GET /ai-usage` | member | — | `200 {"spent_usd": 12.34, "limit_usd": 50, "resets_at"}` | — |
| `GET /issues/{identifier}/test-cases` | member | — | `200 [case]` by position: `{"id","position","title","steps":[],"expected","source","updated_at","last_result":{"status","run_id","kind","at"}\|null}` | 404 |
| `POST /issues/{identifier}/test-cases` | `tests.manage` | `title`, `steps` (repeated field, one per step), `expected` | `201 case` (added last) | 400 `{"errors":{"title":"Enter a title.","steps":"Add at least one step.","expected":"Describe the expected result."}}`; 409 `{"error":"An issue can have at most 50 test cases."}` |
| `POST /test-cases/{id}` | `tests.manage` | `title`, `steps`, `expected` | `200 case` | as above, 404 |
| `POST /issues/{identifier}/test-cases/order` | `tests.manage` | `ids` (repeated, full order) | `200 [case]` | 400 `{"error":"The list changed. Reload and try again."}` |
| `DELETE /test-cases/{id}` | `tests.manage` | — | `204` (soft delete) | 404 |
| `POST /issues/{identifier}/test-runs` | `ai_tests.run` for `kind=ai`, `tests.manage` for `kind=manual` | `kind` (`ai`\|`manual`), `regenerate` (`true` for ai only, optional) | `201 run` | manual: 409 `{"error":"Add a test case first."}`. ai: 409 `{"error":"Add a test site in Settings → AI testing first."}`; 409 `{"error":"This month's AI test limit is reached."}`; 409 `{"error":"A test is already running on this issue."}`; 503 `{"error":"AI testing isn't set up on this server."}` (no tester has checked in: tester upserts a heartbeat key in `system_config` every 30 s) |
| `GET /issues/{identifier}/test-runs` | member | — | `200 [run summary]` newest first, 20 | 404 |
| `GET /test-runs/{id}` | member | — | `200 run` (below) | 404 |
| `POST /test-runs/{id}/results/{result_id}` | `tests.manage`, manual run still `running` | `status` (`passed`\|`failed`\|`skipped`), `note` (optional, ≤ 2000) | `200 run` | 400 `{"errors":{"status":"Choose passed, failed or skipped."}}`; 409 `{"error":"This run is finished."}`; 409 for an ai run |
| `POST /test-runs/{id}/cancel` | the permission that started it | — | `200 run` | 404; 409 when already finished |

A manual run snapshots the issue's cases at start; cases added or edited later show up in the next run. A one-person-at-a-time lock isn't needed: two members marking the same run just both write results (last mark wins per case).

Run JSON:
```json
{"id","kind","issue":{"identifier","title"},"status","error","started_by":{"id","name"},"created_at","finished_at","cost_usd",
 "results":[{"id","case_id","title","steps":[],"expected","status","summary","reproduction":[],"marked_by":{"id","name"},"marked_at",
             "log":[{"position","action","url","screenshot_url"}]}],
 "latest_screenshot_url"}
```
`cost_usd`, `reproduction`, `log`, `latest_screenshot_url` are empty/null on manual runs; `marked_by` null on ai runs.

Realtime: every run write publishes `/api/workspaces/{slug}/test-runs/{id}` and `/api/workspaces/{slug}/issues/{identifier}/test-runs`; case writes (incl. AI generation) publish `/api/workspaces/{slug}/issues/{identifier}/test-cases`; site/account writes publish their list path.

Activity log on the issue: "ran AI tests: 5 passed, 1 failed", "ran tests manually: …", "regenerated test cases" (existing activity table, new kinds).

## Backend

- Package `internal/aitest`: `routes.go`, `controller.go`, `model.go` (all SQL), `seal.go`, `validate.go` (base URL rules), and for the tester `runner.go` (claim loop, case loop), `claude.go` (requests, tools, usage), `cdp.go`, `proxy.go`, `pricing.go`.
- `cmd/tester/main.go`: env `DATABASE_URL`, `ANTHROPIC_API_KEY`, `TEST_SECRET_KEY`, `UPLOAD_DIR`, `CHROME_URL` (default `http://chrome:9222`), `PROXY_ADDR` (default `:3128`), `AI_TEST_BROWSERS`. Refuses to start without the key in production. Graceful shutdown finishes the current step, marks its runs `error` "The test runner restarted. Run it again."
- `docker-compose.yml` (dev): add `chrome` service on an `internal: true` network `browser`; dev tester runs on the host with `go run`, so dev Chrome gets `--proxy-server=host.docker.internal:3128` and the host-gateway extra host. `deploy/docker-compose.prod.yml`: add `tester` (same image, `-tester` entry or separate `SVC`) on default + `browser` networks, `chrome` on `browser` only; add `ANTHROPIC_API_KEY`, `TEST_SECRET_KEY` to `deploy/env.example`.
- Checks: `proxy_test.go` (above); `seal_test.go` (round trip, wrong workspace fails); a model test for the claim query's per-workspace cap and stale-run takeover; a runner test against a fake Claude (interface-free: inject the `func` that sends a request) and a local `httptest` site over real Chrome, skipped when `CHROME_URL` isn't reachable.
- Verify with curl: site/account CRUD and permission 403s, private base URL rejected, password never in any response, run create/409s/cancel; one real run against a tiny staging page with a login, including a deliberately failing case.

## Frontend (user app)

- **Issue page, "Tests" section** under the description: the saved cases (title, expandable steps and expected result, last result icon). With `tests.manage`: "Add test case", edit inline, delete, drag to reorder. Buttons: "Run AI test" (with `ai_tests.run`; disabled with tooltip when no test site; a menu option "Regenerate cases and run" when cases exist, with a confirm "This replaces the current test cases."), and "Run manually" (with `tests.manage`; disabled when there are no cases). Empty state: "No test cases yet. Run an AI test to write them, or add one."
- **Run panel** (side panel on the issue, URL search param `run=<id>` via `validateSearch` so it's shareable): kind, status pill, cost (ai), cancel; results as a list with status icons (green pass, pink fail, grey skipped).
  - AI run: the selected result shows steps, expected result, the step log, and the latest screenshot large (click a log row to see that step's screenshot). Live via the existing refetch-on-publish.
  - Manual run: the selected result shows steps as a numbered list and the expected result, a note field, and "Passed" / "Failed" / "Skip" buttons (keyboard `P` / `F` / `S`, then the next unmarked case is selected). Finished automatically when the last case is marked.
- On a failed case: "Create bug" opens quick-create prefilled: title `Bug: <case title>`, description with summary, reproduction steps as an ordered list, and the failing screenshot as an image node (existing file URL passes `checkDoc`), linked to the issue as "blocks". A person edits and saves; nothing is filed by the AI.
- On a failed manual result, "Create bug" works the same with the case's steps as the reproduction and the note as the summary.
- **Test history** under the cases: last runs (AI or manual, status, counts, when, who, cost for AI), click opens the panel.
- **Settings → AI testing** (new settings page): this month's spend vs limit; test sites list + add/remove; test accounts list + add/edit/remove, password field write-only ("Saved; leave blank to keep").
- `api.ts` gains the calls above. Forms use `noValidate` + `@tether/shared/validate`.

## Verification (integration)

Add a site and an account; run on an issue whose description says "Sign in and create a project"; watch steps and screenshots arrive live in a second tab; the generated cases appear in the Tests section; a case passes; edit a case and add one by hand, run again and see the AI reuse them; run manually, mark pass/fail/skip with keyboard in one tab while a second tab updates, run ends failed, history shows both runs, an old result still shows the case text it ran after the case was edited; "Regenerate cases and run" replaces them; break the staging page and see it fail with reproduction; "Create bug" prefill; cancel mid-run; set the limit to $0.01 and see `limit_reached`; a page that redirects to `http://169.254.169.254/` is blocked in the log; a page with "ignore your instructions" text doesn't change behaviour; `grep` tester logs for the test password returns nothing.
