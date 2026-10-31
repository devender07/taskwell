# GitHub / GitLab PR linking plan

Scope: a workspace connects GitHub and/or GitLab with a webhook. Pull requests (GitHub) and merge requests (GitLab) that mention an issue identifier (`ENG-142`) are linked to that issue and shown on it. When a linked PR opens, the issue can move to a chosen status; when it merges, the issue moves to a chosen done status. Builds on the issues plan; uses this repo's own webhook endpoint, so there's no OAuth app or GitHub App to register.

Out of scope: GitHub App / OAuth install flow, repo pickers, commits and branches without a PR, review status and CI checks, mapping GitHub users to Tether users, creating branches or PRs from Tether, two-way sync (closing an issue doesn't close the PR).

## How linking works

- Identifiers are found with `(?i)\b([a-z][a-z0-9]{1,9})-(\d{1,7})\b` in the PR's **branch name, title and body**, resolved with `canonical_key` (old keys still work) against the hook's workspace. Unknown keys and numbers are ignored. At most 20 issues per PR.
- A link **closes** the issue when the identifier is in the branch name or title, or follows a closing word in the body (`close(s|d)`, `fix(es|ed)`, `resolve(s|d)`, optionally with `:`). Other body mentions are plain references: shown on the issue, never move it.
- Every delivery re-derives the PR's links (an edit that removes `ENG-5` from the title unlinks it).
- Status moves, only on a state *transition* (compared with the stored PR state, so redeliveries do nothing):
  - opened / reopened / ready for review (not a draft) → `open_status` (if set) for closing links whose issue's status category is `backlog` or `unstarted`;
  - merged → `merge_status` (default: the workspace's first `completed` status by position) for closing links whose category isn't `completed` or `canceled`;
  - closed without merge → nothing.
- Moves go through the normal issue update (event, notifications, realtime) with no actor; the status event's data gains `"via": {"provider","repo","number","url"}`.

Identifier regex and closing-word parsing live in one pure function with a table test (`Parse(branch, title, body) []Ref{Key, Number, Closes}`).

## Schema

Migration `backend/user/internal/db/migrations/034_pr_linking.sql`:

```sql
create table git_hooks (
  id              uuid primary key default uuidv7(),
  workspace_id    uuid not null references workspaces on delete cascade,
  provider        text not null check (provider in ('github', 'gitlab')),
  token           text not null unique,  -- random 32 bytes, base64url: the webhook URL's last segment
  secret          text not null,         -- sealed (AES-GCM, see below)
  open_status_id  uuid references statuses on delete set null,
  merge_status_id uuid references statuses on delete set null,
  last_delivery_at timestamptz,
  created_by      uuid references users on delete set null,
  created_at      timestamptz not null default now(),
  unique (workspace_id, provider)
);

create table pull_requests (
  id           uuid primary key default uuidv7(),
  workspace_id uuid not null references workspaces on delete cascade,
  provider     text not null,
  repo         text not null,             -- "owner/name" (GitHub full_name, GitLab path_with_namespace)
  number       int not null,              -- GitHub number, GitLab iid
  url          text not null,
  title        text not null,
  branch       text not null,
  author       text not null,             -- provider login
  state        text not null check (state in ('draft', 'open', 'merged', 'closed')),
  updated_at   timestamptz not null default now(),
  unique (workspace_id, provider, repo, number)
);

create table issue_pull_requests (
  issue_id uuid not null references issues on delete cascade,
  pr_id    uuid not null references pull_requests on delete cascade,
  closes   boolean not null,
  primary key (issue_id, pr_id)
);
create index on issue_pull_requests (pr_id);
```

Secret sealing: the webhook secret must be readable to verify GitHub's HMAC, so it is sealed with AES-GCM under `INTEGRATION_SECRET_KEY` (env, 32 bytes base64; dev falls back to a key derived from `jwt_secret`, like `MFA_ENCRYPTION_KEY`), associated data = workspace id + provider. Reuse `internal/aitest/seal.go`'s approach; move the generic seal/open into a small `internal/secret` package if both use it.

Permission: new `integrations.manage` ("Workspace" group, label "Connect GitHub and GitLab", active). Not in `memberDefaults`.

## API contract

Same conventions as the issues plan (FormData in, JSON out, 401 / 404 workspace / 403).

Integration JSON: `{"provider":"github","connected":true,"webhook_url":"https://…/api/hooks/github/{token}","open_status": status|null,"merge_status": status|null,"last_delivery_at": string|null}` (`webhook_url` = `app_base_url` + `/api/hooks/…`; the secret is never in it).

Pull request JSON: `{"provider","repo","number","url","title","branch","author","state","closes","updated_at"}`.

Single issue JSON (`GET …/issues/{identifier}` and write responses) gains `"pull_requests": [pr…]` newest first. List JSON is unchanged.

| Method & path | Needs | Fields | Success | Failures |
|---|---|---|---|---|
| `GET /api/workspaces/{slug}/integrations` | `integrations.manage` | — | `200 [integration…]` always both providers (`connected:false` with nulls when not set up) | 403 |
| `POST /api/workspaces/{slug}/integrations/{provider}` | `integrations.manage` | — | `200 {"integration": integration, "secret": "…"}` — creates the hook, or on an existing one **regenerates** token and secret (old URL stops working). The secret is shown only in this response. | 403; 404 `{"error":"Unknown provider."}` |
| `POST /api/workspaces/{slug}/integrations/{provider}/settings` | `integrations.manage` | `open_status` (status id or empty = don't move), `merge_status` (status id or empty = default) | `200 integration` | 400 `{"errors":{"open_status"|"merge_status":"Status not found."}}`; 403; 404 not connected `{"error":"GitHub isn't connected."}` |
| `DELETE /api/workspaces/{slug}/integrations/{provider}` | `integrations.manage` | — | `204` (linked PRs stay on issues) | 403; 404 |
| `POST /api/hooks/github/{token}` | public (signature) | GitHub JSON; headers `X-GitHub-Event`, `X-Hub-Signature-256` | `200 {"ok":true}`; `ping` and unhandled events/actions → `200 {"ignored":true}` | `401` bad/missing signature; `404` unknown token; `413` body over 1 MB |
| `POST /api/hooks/gitlab/{token}` | public (token) | GitLab JSON; headers `X-Gitlab-Event: Merge Request Hook`, `X-Gitlab-Token` | as above | `401` wrong token (constant-time compare); `404`; `413` |

Webhook details:
- GitHub: verify `sha256=` HMAC of the raw body with `hmac.Equal` before parsing. Handle `pull_request` actions `opened, edited, reopened, closed, ready_for_review, converted_to_draft, synchronize`. State: `merged` if `pull_request.merged`, else `closed` if `state == "closed"`, else `draft` if `draft`, else `open`.
- GitLab: `object_attributes.action` `open, reopen, update, close, merge`; state from `object_attributes.state` (`opened`→`open` or `draft` when `draft`/`work_in_progress`, `merged`, `closed`); repo = `project.path_with_namespace`, number = `iid`, branch = `source_branch`, author = `user.username`.
- Body read with `http.MaxBytesReader` (1 MB). Each delivery runs in one transaction: lock the hook row, upsert the PR, replace its `issue_pull_requests`, log `pr_linked` / `pr_unlinked` events on issues that gained/lost the link (`{"provider","repo","number","title","url"}`, no actor), then any status moves, then set `last_delivery_at`.
- Realtime: publish each affected issue's path (and `…/integrations` for `last_delivery_at`).
- The webhook route skips auth; `CheckOrigin` already passes requests without browser headers. Rate limiting reuses the existing limiter if one fits; otherwise `ponytail:` none, add per-token limiting if a hook gets abused.
- Never log bodies or secrets; log provider, workspace id, event, action and outcome.

## Backend (`backend/user/internal/gitlink/`)

- New package `gitlink`: `routes.go`, `controller.go` (settings handlers + the two webhook handlers), `model.go` (all SQL), `parse.go` (identifier parsing + provider payload → common `pr` struct), `parse_test.go`.
- Status moves call the issue package's update path (export a small `issue.SetStatus(ctx, tx, issueID, statusID, via)` or similar inside the same transaction) so events, notifications and realtime behave exactly like a manual change; `ActorID` empty means "no actor" (check `addEvents` / `notify.Record` accept it; store `actor_id` null).
- Issue package: single-issue select adds `pull_requests`.
- Verify: `go vet`, `go test` (parse table test; a model test: opened → open status, redelivery no-op, merged → done, removed mention unlinks), and curl with real HMAC-signed bodies (`openssl dgst -sha256 -hmac`) for both providers, plus bad signature 401, unknown token 404, oversized 413.

## Frontend (user app)

`api.ts`: `Integration`, `PullRequest`; `Issue.pull_requests?`; `listIntegrations`, `connectIntegration`, `updateIntegration`, `disconnectIntegration`. Event kinds `pr_linked` / `pr_unlinked` in `event-text.tsx` ("linked GitHub PR acme/web#12"), actorless events read as "GitHub" / "GitLab", and a status event with `via` reads "moved to Done when acme/web#12 merged".

| Where | Behaviour |
|---|---|
| Settings → Integrations (`/{workspace}/settings/integrations`, sidebar entry shown with `integrations.manage`) | One card per provider. Not connected: one line on what it does and "Connect GitHub" / "Connect GitLab". Connected: webhook URL with a copy button, setup steps (GitHub: repo or org Settings → Webhooks → Payload URL, content type `application/json`, secret, "Let me select individual events" → Pull requests. GitLab: project or group Settings → Webhooks → URL, secret token, "Merge request events"), "Last delivery 3 minutes ago" / "No deliveries yet", two status selects ("When a pull request opens" with "Don't move" first; "When it merges" defaulting to the first done status), "Regenerate secret" (confirm: "The old webhook URL and secret stop working.") and "Disconnect" (confirm). After connect/regenerate the secret shows once in a callout with copy: "Copy this secret now. You won't see it again." |
| Issue page | **Pull requests** section under relations when the issue has any: rows with provider icon, `repo#number`, title (links to `url`, new tab), state pill (Draft / Open / Merged / Closed; merged in green), branch in Berkeley Mono. Hidden when empty. |
| Issue page actions | "Copy branch name" (menu item and shortcut `⌘⇧.`): `eng-142-` + title slug (lowercase, ascii, `-`-joined, at most 50 chars). Toast "Branch name copied". Always available, not tied to a connection. |

## Verification

- Backend: above.
- Frontend: `npm run build`.
- Integration (main session): connect GitHub in Settings, copy URL and secret; send signed `pull_request` payloads with curl through the dev proxy: opened with branch `eng-1-fix-login` → ENG-1 shows the PR as Open and moves to the open status; body "Refs ENG-2" links ENG-2 without moving it; merged → ENG-1 moves to Done, activity says "moved to Done when …#12 merged", ENG-2 stays; redelivering the merged payload changes nothing; edited title dropping ENG-1 unlinks it. Same flow once for GitLab. Regenerate secret → old URL 404. A member without `integrations.manage` can't open the settings page (sidebar hidden, API 403).
