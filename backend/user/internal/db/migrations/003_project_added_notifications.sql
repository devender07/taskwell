-- "Added you to a project" notifications: they point at the project instead of an issue or chat.
alter table notifications add column project_id uuid references projects on delete cascade;
create index on notifications (project_id) where project_id is not null;
alter table notifications drop constraint notifications_kind_check,
  add constraint notifications_kind_check
    check (kind in ('mention', 'assigned', 'status', 'comment', 'chat_mention', 'message', 'project_added'));
alter table notifications drop constraint notifications_target_check,
  add constraint notifications_target_check check (num_nonnulls(issue_id, chat_id, project_id) = 1);
