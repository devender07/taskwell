-- Indexes on foreign keys (so deleting a user or workspace doesn't scan these tables), and on created_at for the
-- admin stats' last-30-days and active-users ranges. Built concurrently, outside a transaction, so writes go on;
-- each is dropped first so a rerun after a failure doesn't keep an invalid half-built index.
drop index concurrently if exists notifications_workspace_id_idx;
create index concurrently notifications_workspace_id_idx on notifications (workspace_id);
drop index concurrently if exists notifications_actor_id_idx;
create index concurrently notifications_actor_id_idx on notifications (actor_id);
drop index concurrently if exists users_avatar_file_id_idx;
create index concurrently users_avatar_file_id_idx on users (avatar_file_id);
drop index concurrently if exists workspace_invites_role_id_idx;
create index concurrently workspace_invites_role_id_idx on workspace_invites (role_id);
drop index concurrently if exists workspace_invites_invited_by_idx;
create index concurrently workspace_invites_invited_by_idx on workspace_invites (invited_by);
drop index concurrently if exists project_key_aliases_project_id_idx;
create index concurrently project_key_aliases_project_id_idx on project_key_aliases (project_id);
drop index concurrently if exists comments_author_id_idx;
create index concurrently comments_author_id_idx on comments (author_id);
drop index concurrently if exists issue_events_actor_id_idx;
create index concurrently issue_events_actor_id_idx on issue_events (actor_id);
drop index concurrently if exists issues_created_by_idx;
create index concurrently issues_created_by_idx on issues (created_by);
drop index concurrently if exists files_uploader_id_idx;
create index concurrently files_uploader_id_idx on files (uploader_id);
drop index concurrently if exists projects_created_by_idx;
create index concurrently projects_created_by_idx on projects (created_by);
drop index concurrently if exists workspaces_created_by_idx;
create index concurrently workspaces_created_by_idx on workspaces (created_by);
drop index concurrently if exists issue_viewers_user_id_idx;
create index concurrently issue_viewers_user_id_idx on issue_viewers (user_id);

drop index concurrently if exists users_created_at_idx;
create index concurrently users_created_at_idx on users (created_at);
drop index concurrently if exists issues_created_at_idx;
create index concurrently issues_created_at_idx on issues (created_at);
drop index concurrently if exists refresh_tokens_created_at_idx;
create index concurrently refresh_tokens_created_at_idx on refresh_tokens (created_at);
