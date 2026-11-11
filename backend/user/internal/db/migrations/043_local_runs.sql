-- Local test runs (docs/local-test-runner-plan.md): the user's own agent drives Chrome on their machine through
-- taskwell-tester, which posts the steps, screenshots and a video of each case. client is the agent ("Claude Code").
alter table test_runs drop constraint test_runs_kind_check;
alter table test_runs add constraint test_runs_kind_check check (kind in ('ai', 'manual', 'replay', 'local'));
alter table test_runs add column client text check (char_length(client) between 1 and 40);
alter table test_results add column video_id uuid references files on delete set null;
create index test_results_video_id_idx on test_results (video_id) where video_id is not null;

insert into system_config (key, value, secret, description) values
  ('ai_tests_server_runs', 'false', false, 'Run AI tests, recorded replays and browser sessions on Taskwell''s tester (spends the platform Anthropic key)')
on conflict (key) do nothing;
