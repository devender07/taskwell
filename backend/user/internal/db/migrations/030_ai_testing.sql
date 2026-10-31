-- AI test runs: the sites the tester's Chrome may reach, the accounts it signs in with (passwords sealed with
-- TEST_SECRET_KEY), each step's log and screenshot, and Claude spend per workspace.
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

create table test_steps (
  id            uuid primary key default uuidv7(),
  result_id     uuid not null references test_results on delete cascade,
  position      int not null,
  action        text not null,          -- "click [12] button \"Save\"", never typed text
  url           text,
  screenshot_id uuid references files on delete set null,
  created_at    timestamptz not null default now()
);
create index on test_steps (result_id, position);
create index on test_steps (screenshot_id);

create table ai_usage (
  workspace_id uuid not null references workspaces on delete cascade,
  run_id       uuid references test_runs on delete set null,
  cost_micros  bigint not null,
  created_at   timestamptz not null default now()
);
create index on ai_usage (workspace_id, created_at);
create index on ai_usage (run_id);

insert into system_config (key, value, secret, description) values
  ('ai_test_monthly_limit_usd', '50', false, 'AI test spend per workspace per month, in USD'),
  ('ai_test_workspace_concurrency', '2', false, 'AI test runs one workspace can have running at once'),
  ('ai_tester_heartbeat', '', false, 'When a tester last checked in (set by the tester every 30s; AI runs need one within 90s)')
on conflict do nothing;
