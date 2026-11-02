-- Deleting an issue failed once it had test results: its cases were deleted before the cascade reached their runs'
-- results, and test_results.case_id had no delete rule. Cases are only hard-deleted with their issue (deleting one
-- sets deleted_at), so their results go with them.
alter table test_results drop constraint test_results_case_id_fkey;
alter table test_results add constraint test_results_case_id_fkey foreign key (case_id) references test_cases on delete cascade;
