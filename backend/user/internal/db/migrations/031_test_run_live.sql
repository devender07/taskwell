-- Where the API reaches the tester running this AI run, for the live browser view.
alter table test_runs add column tester_url text;
