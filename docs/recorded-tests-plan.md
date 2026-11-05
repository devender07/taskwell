# Recorded tests plan

Scope: test cases whose browser steps are written by an MCP client (Claude Code, on the client's own plan) and saved on the case, replayed by Taskwell's tester **without Claude**, and exported as a Playwright spec a developer can run and fix locally. The platform Anthropic key is spent only when someone explicitly starts an AI run.

Builds on the existing recording: `test_cases.script` (032) holds a passed AI case's steps, and AI runs already replay them before falling back to Claude. This plan adds: saving a script through the API, a run kind that only replays (never Claude, never the spend limit), in-script assertions, the Playwright export, MCP tools and two buttons.

Out of scope: running Playwright itself in the tester (the export is for developers' machines), editing scripts in the UI, recording from the user's own browser, scheduling runs.

## Script format (extends `script` in `aitest/replay.go`)

```json
{"steps": [
  {"tool": "navigate", "url": "https://lapp.vishnugrowth.com/login"},
  {"tool": "type", "target": {"css": "input[name=\"email\"]", "tag": "input", "name": "Email"}, "text": "qa@vishnugrowth.com"},
  {"tool": "fill_password", "target": {"css": "", "tag": "input", "name": "Password"}, "account": "QA user"},
  {"tool": "click", "target": {"css": "", "tag": "button", "name": "Sign in"}},
  {"tool": "assert_text", "text": "My inbox"},
  {"tool": "assert_no_text", "text": "Draft A - My inbox"}
 ],
 "check": "Drafts"}
```

- Existing tools: `navigate`, `click`, `type`, `select`, `press`, `wait`, `fill_password` (by test account label; the password never appears in a script).
- New tools: `assert_text` and `assert_no_text` (`text`): the replay waits up to 5 s for the text to appear / disappear from `document.body.innerText`; failing ends the case as failed with "Expected \"…\" on the page." / "Expected \"…\" not to be on the page." Negative checks are what most edge cases need (a draft must *not* show in the other inbox).
- `target`: `css` (a selector matching one element, may be empty) plus `tag` and `name` (the accessible name as `nameOf` in `cdp.go` computes it: aria-label, label text, placeholder, inner text, button value, title, name attribute). Replay finds it the same way it does today (`resolveJS`).
- `check` stays: the final text that proves the expected result.

Validation (`validate.go`, 400 `{"errors":{"script":"Step 3: …"}}`):
- 1–60 steps; the first is `navigate`; `check` 1–200 chars.
- `navigate.url` is http(s) on one of the workspace's test site hosts (`allowedHost`), else `Step N: https://x isn't a registered test site. Add it in Settings → AI testing.`
- `click`/`type`/`select`/`fill_password` need `target.tag` and (`target.css` or `target.name`); `css` ≤ 500, `name` ≤ 200.
- `type.text` ≤ 1000; `select.value` ≤ 200; `press.key` ≤ 30; `wait.seconds` 1–5; `assert_*.text` 1–200.
- `fill_password.account` is the label of one of the workspace's test accounts.
- Unknown tool → `Step N: unknown step "x".`

## Schema

Migration `036_recorded_runs.sql`:

```sql
alter table test_runs drop constraint test_runs_kind_check;   -- name as Postgres generated it; check with \d
alter table test_runs add constraint test_runs_kind_check check (kind in ('ai', 'manual', 'replay'));
```

No `system_config` keys. Editing a case still clears its script (existing behaviour).

## API contract (user service, `internal/aitest`)

| Method & path | Needs | Fields | Success | Failures |
|---|---|---|---|---|
| `POST /api/workspaces/{slug}/test-cases/{id}/script` | `tests.manage` | `script` (the JSON above) | `200 case` (`recorded: true`) | 400 `{"errors":{"script": msg}}` (also `Send the script as JSON.`); 403; 404 `errCaseNotFound` |
| `DELETE /api/workspaces/{slug}/test-cases/{id}/script` | `tests.manage` | — | `204` | 403; 404 |
| `POST /api/workspaces/{slug}/issues/{identifier}/test-runs` (changed) | `tests.manage` for `kind=replay` | `kind=replay` | `201 run` | 409 `{"error":"Record a test case first."}` (no case has a script); 409 `A test is already running on this issue.`; 409 `Add a test site in Settings → AI testing first.`; 503 `AI testing isn't set up on this server.` (no live tester) |
| `GET /api/workspaces/{slug}/issues/{identifier}/playwright` | member | — | `200 text/plain; charset=utf-8`, `Content-Disposition: attachment; filename="VIS-3.spec.ts"` | 404 issue |

Replay runs:
- Queued and claimed by the tester like AI runs (claim, per-workspace concurrency, cancel, live view and stale-run takeover cover `kind in ('ai','replay')`; one active `ai` or `replay` run per issue).
- **No spend-limit check and no Claude call, ever.** A recorded case replays; a failed replay or assertion is `failed` with the reason as summary and the steps done so far (`describe()`) as reproduction; a case without a script is `skipped` with "Not recorded yet. Record it with Claude Code or an AI run." Cost stays 0.
- Run JSON `kind: "replay"`.

Playwright export (`playwright.go`, deterministic, no AI):
- Header comment: issue identifier and title, the test site(s), how to run (`npm i -D @playwright/test && npx playwright test VIS-3.spec.ts`), and the env vars for passwords.
- `import { test, expect } from '@playwright/test';` then `test.describe('VIS-3 <title>', …)` with one `test('<case title>', async ({ page }) => { … })` per recorded case, steps as comments above the code (`// 1. Open My inbox and click Compose`), `// Expected: …` at the end.
- Mapping: `navigate` → `await page.goto(url)`; target → `page.locator(css)` when css, else `page.getByRole(role, { name, exact: true })` for button/link/textbox/combobox/checkbox roles from the tag, else `page.locator(tag, { hasText: name }).first()`; `click` → `.click()`; `type` → `.fill(text)`; `select` → `.selectOption(value)`; `press` → `page.keyboard.press(key)`; `wait` → `page.waitForTimeout(ms)`; `fill_password` → `.fill(process.env.TASKWELL_PASSWORD_<LABEL_AS_CONST> ?? '')`; `assert_text` / `check` → `await expect(page.getByText(text).first()).toBeVisible()`; `assert_no_text` → `await expect(page.getByText(text)).toHaveCount(0)`.
- Unrecorded cases → `test.fixme('<title>', async () => {})` with their steps and expected result as comments, so the developer sees what's missing.
- Strings escaped as JS string literals (JSON encoding).
- Runnable check: `playwright_test.go` builds a spec from a script covering every tool and asserts the lines; plus validation table tests.

## MCP (`backend/mcp`)

| Tool | Inputs | Calls |
|---|---|---|
| `save_test_script` | `workspace`, `case_id`, `steps` ([]step objects as above), `check` | `POST …/test-cases/{id}/script` |
| `clear_test_script` | `workspace`, `case_id` | `DELETE …/test-cases/{id}/script` |
| `run_recorded_tests` | `workspace`, `identifier` | `POST …/test-runs` `kind=replay`; then `get_test_run` |
| `get_playwright_spec` | `workspace`, `identifier` | `GET …/playwright` (returns the code as text) |

`save_test_script`'s description is the recording guide: explore the site with your own browser tools first (Taskwell doesn't spend AI here); prefer stable selectors (`data-testid`, `id` without long digit runs, `name`, `aria-label`, `placeholder`) and always give `tag` + accessible `name` as the fallback; never put a password in a step, use `fill_password` with a label from `list_test_accounts` (ask the user to add one in Settings → AI testing when there is none); add `assert_text` / `assert_no_text` after the steps that matter; finish with a `check` text that only appears when the expected result holds; then call `run_recorded_tests` to prove it passes. `list_test_cases` and the tests view already show `recorded`; the view labels `replay` runs "Recorded test run".

## Frontend (user app, `components/tests/tests.tsx`)

- `api.ts`: `RunKind` gains `'replay'`; `startRecordedRun(slug, identifier)`; `playwrightURL(slug, identifier)`.
- Tests section header: **"Run recorded tests"** button next to the existing run buttons (`tests.manage`), disabled with title "Record a test case first." when no case is recorded; starts a `replay` run and opens the run panel like an AI run (live view, step log, screenshots).
- **"Download Playwright"** link (`<a href download>`) in the same header when the issue has at least one case.
- Run history and run panel: label `replay` runs "Recorded", no cost line.
- Copy (sentence case): "Run recorded tests", "Download Playwright", "Recorded", "Not recorded yet. Record it with Claude Code or an AI run."

## Verification

- Backend: `go vet`, `go test` (validation table, Playwright golden lines, a model test that a replay run is claimed and never checks the limit); curl every row and message; a replay run end to end against a local test site with `make` running (Chrome + tester), including an `assert_no_text` failure.
- MCP: `go test` with the fake API for `save_test_script` (steps sent as the JSON field) and `get_playwright_spec`.
- Frontend: `npm run build`.
- Integration (main session): record a case on a local site through MCP, run recorded tests from the UI with the AI limit at 0 (proves no key/limit use), break the page and see a failed result with screenshot, download the spec and run it with Playwright.

## Addendum: Playwright code written by the client

MCP clients can drive a real browser with `playwright-cli` (installed on the user's machine), which prints the Playwright TypeScript for every action (`getByRole(...)` locators) and lets the client add real assertions (`toHaveText`, `toMatchAriaSnapshot`, …). That code is better for developers than what we generate from a Taskwell script, so Taskwell stores it too.

- Migration 036 also adds `alter table test_cases add column playwright text check (char_length(playwright) <= 20000);`. Editing a case clears it, like `script`.
- `POST …/test-cases/{id}/script` takes an optional `playwright` field: the case's test as Playwright TypeScript, either the body of the test or a whole `test('…', async ({ page }) => { … })` block. Rules: ≤ 20000 chars (`playwright: Use 20000 characters or fewer.`). Passwords must come from `process.env.TASKWELL_PASSWORD_<LABEL>` (the export's naming): a line that mentions `password` (case-insensitive) and calls `.fill(` with a string literal is rejected with `playwright: Use process.env for passwords, never a literal.` The field present replaces the stored code (empty clears it); absent leaves it unchanged.
- Case JSON gains `"has_playwright": bool`.
- Export: per case, the stored code verbatim (wrapped in `test('<case title>', async ({ page }) => { … })` when it's a body, detected by not starting with `test(`), else generated from the script, else `test.fixme`.
- MCP `save_test_script` gains `playwright` (string). Its description adds: when `playwright-cli` is available, record with it (`snapshot` for refs, actions print the Playwright code, `generate-locator` for assertion locators), send that code as `playwright` and translate each action to a Taskwell step for the no-AI replay: role `button`/`link`/`textbox`/`combobox`/`checkbox` + accessible name → `target {tag, name}` (check the real tag with `playwright-cli eval "el => el.tagName" eN`, and prefer a `data-testid`/`id`/`name` selector as `css` when `eval` shows one).
