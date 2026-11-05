-- Files attached to an issue: listed under its description, not inside it. They go with the issue when it's purged.
alter table files add column issue_id uuid references issues_all(id) on delete cascade;
create index on files (issue_id) where issue_id is not null;
