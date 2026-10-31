-- Home's activity feed reads events (but description edits) and comments newest first across the workspace.
drop index concurrently if exists issue_events_activity_idx;
create index concurrently issue_events_activity_idx on issue_events (created_at) where kind <> 'description';
drop index concurrently if exists comments_created_at_idx;
create index concurrently comments_created_at_idx on comments (created_at);
