# Local test runner plan

Scope: AI test runs that happen on the user's machine with the user's own coding agent (Claude Code, Cursor, Codex), not on Taskwell's server and not on Taskwell's Anthropic key. The user installs a small MCP server, **taskwell-tester**. Their agent decides each action; the runner drives Chrome on their machine and itself sends every step, screenshot and a video of each case to Taskwell. Taskwell only stores and shows the record.

Server-side runs (AI runs, recorded replays, hosted browser sessions) are switched off by default but the code stays.

Out of scope:
- Replaying recorded cases locally without an agent, and saving a local run as a recorded script. Both fit the runner later.
- Removing the tester binary, the Chrome container or the Anthropic key.
- Pricing page and plan copy that mention an AI testing budget. Revisit once this ships.
- Trusting the record beyond "the runner made it". A user who edits the runner can still fake a run.

## Why a local runner and not the hosted MCP alone

The hosted MCP server can't see the user's machine: it can't open their browser, reach `localhost`, or read a screenshot file. A program on the user's machine can do all three, and it means a local run can test a dev build before it is deployed, which the server tester never could.

## Decision to confirm: the runner is a Node package

`runner/` at the repo root, published to npm as `taskwell-tester`, run with `npx`. Dependencies: `playwright-core` and `@modelcontextprotocol/sdk`. This is a new stack and two new dependencies, so it needs a yes.

Why not a Go binary reusing `aitest/cdp.go`:
- Video needs an encoder. Playwright ships one; the Go code would need ffmpeg installed on every user's machine.
- A Go binary has to be built for three systems and signed, or macOS blocks it. `npx` needs neither, and every Claude Code user already has Node.
- Playwright's waiting and element lookup are more dependable than the hand-written CDP code, which is the likeliest source of today's flaky runs.

## Schema

`043_local_runs.sql`:

```sql
alter table test_runs drop constraint test_runs_kind_check;
alter table test_runs add constraint test_runs_kind_check check (kind in ('ai', 'manual', 'replay', 'local'));
alter table test_runs add column client text check (char_length(client) between 1 and 40);  -- "Claude Code"
alter table test_results add column video_id uuid references files on delete set null;
insert into system_config (key, value, secret, description) values
  ('ai_tests_server_runs', 'false', false, 'Run AI tests, recorded replays and browser sessions on Taskwell''s tester (spends the platform Anthropic key)');
```

`feature_ai_testing` stays on: it gates test cases and every kind of run, so it can't be the switch.

## API contract (user service, `internal/aitest`)

All need `tests.manage` and work with a personal API token.

| Method & path | Fields | Success | Failures |
|---|---|---|---|
| `POST /api/workspaces/{slug}/issues/{identifier}/test-runs` (changed) | `kind=local`, `client` (1–40 chars), optional `case_id` | `201 run`, status `running`, one `pending` result per case | 400 `{"errors":{"client":"…"}}`; 409 `Add a test case first.`; 409 `A test is already running on this issue.` |
| `POST /api/workspaces/{slug}/test-runs/{id}/results/{result_id}/steps` (new) | multipart: `action` (1–300 chars), `url` (≤ 2000, optional), `screenshot` (JPEG or PNG file, optional) | `201 {"position": n}` | 400 field errors; 404; 409 `Only local runs take steps.`; 409 `errRunFinished`; 409 `This result is already marked.`; 413 over 2 MB; 409 `A test case can have 60 steps.` |
| `POST /api/workspaces/{slug}/test-runs/{id}/results/{result_id}/video` (new) | `upload_id`: a finished upload from `POST /api/workspaces/{slug}/uploads` (WebM or MP4, ≤ 10 MB) | `200 run` | 400 `{"errors":{"upload_id":"…"}}`; 404; 409 as above |
| `POST /api/workspaces/{slug}/test-runs/{id}/results/{result_id}` (changed) | `status`, `note`, and for local runs `reproduction` (repeated, ≤ 30 lines of ≤ 300 chars) | `200 run` | unchanged; `local` runs are accepted like `manual` |

Rules:
- A local run needs no test site, no live tester and no spend check, and costs 0. Any URL is allowed, `localhost` included: the browser is the user's.
- One active `ai`, `replay` or `local` run per issue.
- The step endpoint has its own 2 MB body cap. It stores the screenshot the way `addStep` does and publishes the run, so the issue page updates live.
- A local run with no step or mark for 15 minutes is ended by `failStale` as `error` with "The local runner stopped. Run it again."
- Cancel works as today. The runner learns of it from the 409 on its next call.
- Run JSON gains `client`; `kind` can be `local`. Result JSON gains `video_url`. `addLogs` fills step logs for local runs too.
- With `ai_tests_server_runs` false: `kind=ai`, `kind=replay` and `POST …/browser-sessions` answer 409 `Tests now run on your own machine. Set up taskwell-tester in Settings → AI testing.` The tester's claim loop takes nothing. `GET /api/workspaces/{slug}/ai-usage` gains `server_runs` (bool) so the app knows.

Checks: Go tests for the local run lifecycle (create, steps with and without a screenshot, video, mark with reproduction, finish, stale end, cancel, the switch refusing `ai` and `replay`), and that a `manual` run still refuses steps.

## Runner (`runner/`, npm `taskwell-tester`)

A stdio MCP server. Setup, shown in the app with a copy button:

```sh
claude mcp add taskwell-tester -e TASKWELL_TOKEN=tth_… -- npx -y taskwell-tester
```

Env: `TASKWELL_TOKEN` (personal API token, required), `TASKWELL_URL` (default the production app), `TASKWELL_HEADLESS=1` (default: a visible window, so the user watches), `TASKWELL_PASSWORD_<LABEL>` (test account passwords, the same names the Playwright export uses).

Tools:

| Tool | Does |
|---|---|
| `start_test_run` | `workspace`, `identifier`, optional `case_id`. Creates the local run and returns each case's `result_id`, steps and expected result. Its description is the guide: test each case in order with `begin_case`, the browser tools, then `finish_case`. |
| `begin_case` | `result_id`, `url`. Opens a fresh browser context recording video and goes to the URL. Answers with the page: URL, title, numbered interactive elements and visible text. |
| `browser_navigate`, `browser_click`, `browser_type`, `browser_select`, `browser_press`, `browser_wait`, `browser_assert_text`, `browser_assert_no_text` | Same inputs and page answer as the hosted `browser_*` tools. Each one that succeeds is posted to Taskwell as a step with a screenshot. A failed action posts nothing and returns the error. |
| `browser_fill_password` | `ref`, `account`. Types `TASKWELL_PASSWORD_<LABEL>`; the agent never sees it and the step says only "Typed the password for <label>". |
| `browser_snapshot`, `browser_screenshot` | Read the page; post nothing. |
| `finish_case` | `result_id`, `status` (passed, failed, skipped), `summary`, `reproduction` for a failure. Closes the context, uploads the video, marks the result. The run finishes with the last case. |
| `cancel_test_run` | Cancels the run and closes the browser. |

Details:
- Chrome: the user's installed Chrome (`channel: 'chrome'`), else Playwright's Chromium with a one-line install hint in the error.
- Video: 1280×720 WebM from Playwright. `ponytail:` a video over 10 MB is dropped and the result's note says so; lower the frame size or split by step if that turns out common.
- Text typed into `type="password"` fields is never posted in a step.
- The runner holds one run at a time. Closing the agent or the window leaves the run for `failStale`.
- Check: `runner/check.mjs` runs the tools against a small local page and a fake Taskwell API, and asserts the steps, screenshots, video upload and marks it received.

## Hosted MCP (`backend/mcp`)

- `get_test_run` and `list_test_runs` return `kind: "local"`, `client` and each result's `video` URL.
- `run_ai_test`, `run_recorded_tests` and `browser_open` pass the 409 through. Their descriptions gain: "Off on most servers; use taskwell-tester on your machine instead."
- The tests view (MCP Apps) labels local runs and links the video.

## Frontend (user app)

- Issue page, Tests (`components/tests/tests.tsx`): with `server_runs` false, "Run AI test" and "Run recorded tests" are replaced by **Run with your Claude**, a dialog with the setup command and a prompt to paste into the agent ("Test ENG-12 with taskwell-tester"), both with copy buttons. Manual runs stay.
- Run view: a local run reads "Local run · <client> · <person>". Its results show the step log with screenshots as AI runs do, live while it runs, and a `<video controls>` player when `video_url` is set.
- Settings → AI testing: a "Run on your machine" section with the same setup command and a link to create an API token. With `server_runs` false the spend, test sites and test accounts sections are hidden.

## Admin app

Config → the `ai_tests_server_runs` switch, labelled "Run tests on Taskwell's servers", with the note that it spends the platform Anthropic key.
