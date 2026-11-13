# Browser sessions plan

Scope: MCP clients record test cases by driving **Taskwell's sandboxed test browser** step by step, so users need no local Playwright and no AI spend: the client's own model decides each action, the tester executes it and keeps the replayable step, and `browser_save` stores the steps on a test case (then "Run recorded tests" replays them, "Download Playwright" exports them). Builds on `docs/recorded-tests-plan.md` (script format, validation, replay, export) and the tester (`docs/ai-testing-plan.md`).

Out of scope: a UI for sessions (MCP only), live view of sessions, sessions surviving a tester restart, more than one tab, file uploads, recording from the user's own browser.

## How it works

- The user API creates a session by calling the tester directly over the internal network (like the live view does). The tester address comes from the heartbeat: the tester writes its `LIVE_URL` as the `ai_tester_heartbeat` value (today the value is empty). `ponytail:` the last tester to beat gets new sessions; spread over testers by load if there are several.
- The tester keeps each session in memory: one page in a fresh browser context through the egress proxy (same host rules as runs: the workspace's test sites only, private hosts only in dev), the workspace's sites and accounts, and the steps recorded so far (`[]scriptStep`, recorded the same way AI runs record: `locate(ref)` for targets).
- Browser slots are shared with runs: a session takes one of the tester's `AI_TEST_BROWSERS` slots and counts toward `ai_test_workspace_concurrency`. Full → the tester answers 409 and the API says `The test browser is busy. Try again in a minute.`
- A session closes after 10 minutes without an action, after 30 minutes in total, on `save`, on `DELETE`, or when the tester stops. A closed or unknown session is 404 `This browser session ended. Open a new one.`
- Every tester call carries the session's random token (created by the tester, stored by the user API, never shown to clients) in `X-Taskwell-Session-Token`; the tester rejects calls without the right token, so reaching the internal port isn't enough to drive a session.
- The egress proxy must accept a session's page the way it accepts a run's (its Proxy-Authorization names the session), with the same allow list.

## Schema

Migration `039_browser_sessions.sql`:

```sql
create table browser_sessions (
  id           uuid primary key,            -- the tester's session id
  workspace_id uuid not null references workspaces on delete cascade,
  issue_id     uuid not null references issues on delete cascade,
  started_by   uuid not null references users on delete cascade,
  tester_url   text not null,
  token        text not null,               -- X-Taskwell-Session-Token
  created_at   timestamptz not null default now(),
  closed_at    timestamptz
);
create index on browser_sessions (started_by) where closed_at is null;
```

No new `system_config` keys (`ai_tester_heartbeat` changes meaning: value = tester URL).

## Tester internal API (on `LIVE_ADDR`, internal only)

| Method & path | Body (JSON) | Answer |
|---|---|---|
| `POST /sessions` | `{"workspace_id","url"}` | `201 {"id","token","page"}`; 409 busy; 400 `{"error"}` (URL not a test site) |
| `POST /sessions/{id}/actions` | `{"action", "ref", "text", "url", "value", "key", "seconds", "account"}` | `200 {"page", "error"?}` (`error` = the action failed; nothing recorded) |
| `GET /sessions/{id}/screenshot` | — | `200 image/jpeg` |
| `POST /sessions/{id}/close` | — | `200 {"steps": [scriptStep…]}` and the session is gone |

`page` = `{"url","title","elements"}`: `elements` is the text the AI runs already give Claude (`stateJS`: numbered interactive elements, never a password's value), plus the visible text truncated to 4000 characters so clients can write assertions.

Actions: `navigate {url}`, `click {ref}`, `type {ref,text}`, `select {ref,value}`, `press {key}`, `wait {seconds 1-5}`, `fill_password {ref, account}` (an account allowed on this page's host, as in runs), `assert_text {text}`, `assert_no_text {text}` (wait up to 5 s like replay; a failed assertion is an `error` and isn't recorded), `snapshot` (records nothing). Every successful action except `snapshot` appends its step.

## User API contract (`internal/aitest`)

FormData in, JSON out, same error shapes. All need `tests.manage`; only the user who opened a session can use it (anyone else gets the 404).

| Method & path | Fields | Success | Failures |
|---|---|---|---|
| `POST /api/workspaces/{slug}/issues/{identifier}/browser-sessions` | `url` (optional; default the first test site's base URL) | `201 {"id", "page"}` | 400 `{"errors":{"url":"Use one of this workspace's test sites."}}`; 409 `Add a test site in Settings → AI testing first.` / `The test browser is busy. Try again in a minute.`; 503 `AI testing isn't set up on this server.`; 403; 404 issue |
| `POST /api/workspaces/{slug}/browser-sessions/{id}/actions` | `action`, `ref`, `text`, `url`, `value`, `key`, `seconds`, `account` | `200 {"page", "error"?, "steps": n}` | 400 `{"errors":{"action":"Choose an action."}}`; 404 ended |
| `GET /api/workspaces/{slug}/browser-sessions/{id}/screenshot` | — | `200 image/jpeg` | 404 |
| `POST /api/workspaces/{slug}/browser-sessions/{id}/save` | `case_id`, `check` | `200 case` (recorded); closes the session; the steps go through the same `checkScript` and storage as `POST …/test-cases/{id}/script` (stored Playwright code is cleared, so the export generates it from the steps) | 400 `{"errors":{"script"|"check": msg}}` (session stays open so the client can fix it); 404 |
| `DELETE /api/workspaces/{slug}/browser-sessions/{id}` | — | `204` | 404 |

## MCP tools (`backend/mcp`)

`browser_open(workspace, identifier, url?)`, `browser_click(workspace, session_id, ref)`, `browser_type(…, ref, text)`, `browser_select(…, ref, value)`, `browser_press(…, key)`, `browser_navigate(…, url)`, `browser_wait(…, seconds)`, `browser_fill_password(…, ref, account)`, `browser_assert_text(…, text)`, `browser_assert_no_text(…, text)`, `browser_snapshot(…)`, `browser_screenshot(…)` (image content), `browser_save(…, case_id, check)`, `browser_close(…)`. Each action returns the page (url, title, numbered elements, text) as text. Descriptions explain the flow: open on the issue, sign in with `browser_fill_password` (accounts from `list_test_accounts`; never ask the user for a password), follow the case's steps, add `browser_assert_*` for the expected result (negative checks for "must not show"), `browser_save` to the case, then `run_recorded_tests`. Mention that this needs nothing installed and spends no Taskwell AI; `playwright-cli` stays an option when it's available.

## Verification

- `make vet`, `make test`: a tester test (needs Chrome, like `TestRunner`) that opens a session on an `httptest` site, signs in with `fill_password`, clicks, asserts, closes and gets the steps; that a wrong token is refused and an idle session expires (short timeout injected); a user API test for ownership (another user gets 404) and `save` going through `checkScript`; MCP fake-API test for one action and `browser_save`.
- Live (alongside the user's running `make`: use the separate test database and spare ports as earlier verifications did, so the user's tester doesn't take your sessions): through MCP, open a session on a local test site, sign in with a sealed account, record a case with an `assert_no_text`, save it, run recorded tests and see it pass with the AI limit at 0.
