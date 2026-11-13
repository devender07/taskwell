# taskwell-tester

Runs a Taskwell issue's test cases in Chrome on your machine, driven by your own coding agent (Claude Code, Cursor,
Codex). The agent decides each action; taskwell-tester drives the browser and sends the record to Taskwell, where the
run shows live on the issue. It can test `localhost`, so a dev build can be tested before it is deployed.

## Setup

```sh
claude mcp add taskwell-tester -e TASKWELL_TOKEN=tth_… -- npx -y taskwell-tester
```

Then ask your agent: "Test ENG-12 with taskwell-tester".

Needs Node 20+ and Google Chrome. Each case is recorded as a video, which needs Playwright's ffmpeg build once
(about 2 MB, kept in Playwright's cache folder, nothing system-wide):

```sh
npx -y playwright-core install ffmpeg
```

Without it, or without Chrome, `start_test_run` answers with the exact command to run and starts nothing.

## Settings (environment variables)

| Variable | Meaning |
| --- | --- |
| `TASKWELL_TOKEN` | Your personal API token (Settings → API tokens). Required; your role needs to manage tests. |
| `TASKWELL_URL` | The Taskwell API. Default `https://api.taskwell.dev`. |
| `TASKWELL_HEADLESS` | `1` hides the browser. Default: a visible window, so you can watch. |
| `TASKWELL_PASSWORD_<LABEL>` | A test account's password, typed by `browser_fill_password`. The label in capitals, anything else as `_`: "QA admin" is `TASKWELL_PASSWORD_QA_ADMIN` (the names the Playwright export uses). |

## What is sent to Taskwell

Only to `TASKWELL_URL`, with your token, and only during a run:

- The run: the workspace, the issue and your agent's name (such as "claude-code").
- Each action that worked: a one-line description ("Clicked button "Save""), the page's URL and a JPEG screenshot
  of the page. Text typed with `browser_type` is not in the description, though it can be visible in the screenshot.
- Per case: a 1280×720 WebM video of the browser (dropped when over 10 MB, and the result's note says so), then the
  outcome: passed, failed or skipped, the agent's summary and, for a failure, the steps to reproduce it.

Never sent: the passwords in `TASKWELL_PASSWORD_*` (they are typed into the page on your machine; the step says only
"Typed the password for <label>", and they are masked in the page text your agent reads), actions that failed, and
anything from `browser_snapshot` or `browser_screenshot`. Screenshots and the video show whatever the page shows, so
test with test accounts and test data.

Each case runs in a fresh browser profile: no cookies, logins or extensions from your own Chrome.

## Check

```sh
npm install && npx playwright-core install ffmpeg && npm test
```

`check.mjs` drives the real tools headless against a local page and a fake Taskwell API.
