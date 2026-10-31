-- Testing: test cases saved on an issue, and test runs (manual now, AI later) with one result per case. test_runs
-- already has the AI-only columns so 030_ai_testing.sql doesn't alter it.
create table test_cases (
  id         uuid primary key default uuidv7(),
  issue_id   uuid not null references issues on delete cascade,
  position   int not null,
  title      text not null,
  steps      text[] not null,
  expected   text not null,
  source     text not null check (source in ('ai', 'manual')), -- who wrote it; editing an AI case keeps 'ai'
  created_by uuid references users on delete set null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  deleted_at timestamptz                                       -- soft delete so old results still show the case they ran
);
create index on test_cases (issue_id, position) where deleted_at is null;
create index on test_cases (created_by);

create table test_runs (
  id                 uuid primary key default uuidv7(),
  workspace_id       uuid not null references workspaces on delete cascade,
  issue_id           uuid not null references issues on delete cascade,
  kind               text not null check (kind in ('ai', 'manual')),
  regenerate         boolean not null default false,   -- ai: write new cases first
  started_by         uuid references users on delete set null,
  -- ai: queued, generating, running, then passed/failed/error/cancelled/limit_reached; manual: running, then passed/failed/cancelled
  status             text not null default 'queued',
  error              text,                             -- user-facing message for error / limit_reached
  cancel_requested   boolean not null default false,
  proxy_token        text,                             -- ai only: random, for the egress proxy
  claimed_at         timestamptz,
  finished_at        timestamptz,
  input_tokens       bigint not null default 0,
  output_tokens      bigint not null default 0,
  cache_read_tokens  bigint not null default 0,
  cache_write_tokens bigint not null default 0,
  cost_micros        bigint not null default 0,
  created_at         timestamptz not null default now()
);
create index on test_runs (status, created_at) where status = 'queued';
create index on test_runs (issue_id, created_at desc);
create index on test_runs (workspace_id) where status in ('generating', 'running');
create index on test_runs (started_by);

-- One row per case per run. Title, steps and expected are copied in, so a result still reads right after the case
-- is edited.
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
  marked_by    uuid references users on delete set null,
  marked_at    timestamptz,
  unique (run_id, case_id)
);
create index on test_results (case_id, marked_at desc);
create index on test_results (marked_by);
