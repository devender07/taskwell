-- A case's recorded steps from its last AI pass, replayed by later runs without Claude (null: not recorded; editing
-- the case clears it), and the one case an AI run was started for (null: every case).
alter table test_cases add column script jsonb;
alter table test_runs add column case_id uuid references test_cases on delete set null;
