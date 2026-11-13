-- The test run and result statuses the aitest package writes (aitest/validate.go's finished and the model's activeRun).
alter table test_runs add constraint test_runs_status_check
  check (status in ('queued', 'generating', 'running', 'passed', 'failed', 'error', 'cancelled', 'limit_reached'));
alter table test_results add constraint test_results_status_check
  check (status in ('pending', 'running', 'passed', 'failed', 'skipped', 'error'));
