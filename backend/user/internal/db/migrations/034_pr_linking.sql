-- GitHub / GitLab webhooks per workspace, and the pull requests they link to issues (docs/pr-linking-plan.md).
create table git_hooks (
  id               uuid primary key default uuidv7(),
  workspace_id     uuid not null references workspaces on delete cascade,
  provider         text not null check (provider in ('github', 'gitlab')),
  token            text not null unique,  -- random 32 bytes, base64url: the webhook URL's last segment
  secret           text not null,         -- sealed with INTEGRATION_SECRET_KEY
  open_status_id   uuid references statuses on delete set null,
  merge_status_id  uuid references statuses on delete set null,
  last_delivery_at timestamptz,
  created_by       uuid references users on delete set null,
  created_at       timestamptz not null default now(),
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
