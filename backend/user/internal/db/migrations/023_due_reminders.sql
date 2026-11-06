-- Due-date reminders (docs/due-reminders-plan.md): the assignee of an open issue gets 'due_soon' the day before it's
-- due and 'overdue' after, from nobody (actor null). data is {"due_date": "YYYY-MM-DD"}, one of each per due date.
alter table notifications drop constraint notifications_kind_check,
  add constraint notifications_kind_check check (kind in ('mention', 'assigned', 'status', 'comment', 'chat_mention',
    'message', 'project_added', 'invite', 'due_soon', 'overdue'));
alter table notification_settings add column due_dates boolean not null default true;
create index on notifications (issue_id, kind) where kind in ('due_soon', 'overdue');
