-- Replay runs: the tester replays recorded cases without Claude and without the spend limit (docs/recorded-tests-plan.md).
alter table test_runs drop constraint test_runs_kind_check;
alter table test_runs add constraint test_runs_kind_check check (kind in ('ai', 'manual', 'replay'));

-- The case's test as Playwright TypeScript, written by an MCP client (playwright-cli); editing the case clears it.
alter table test_cases add column playwright text check (char_length(playwright) <= 20000);
