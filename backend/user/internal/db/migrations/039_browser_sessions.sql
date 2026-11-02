-- MCP clients record test cases by driving the tester's browser step by step (docs/browser-sessions-plan.md). The
-- tester keeps the session in memory; this row ties it to its workspace, issue and the user who opened it.
create table browser_sessions (
  id           uuid primary key,            -- the tester's session id
  workspace_id uuid not null references workspaces on delete cascade,
  issue_id     uuid not null references issues on delete cascade,
  started_by   uuid not null references users on delete cascade,
  tester_url   text not null,
  token        text not null,               -- X-Tether-Session-Token
  created_at   timestamptz not null default now(),
  closed_at    timestamptz
);
create index on browser_sessions (started_by) where closed_at is null;

update system_config set description = 'The URL of the tester that last checked in, and when (updated_at; every 30s, AI runs need one within 90s)'
  where key = 'ai_tester_heartbeat';
