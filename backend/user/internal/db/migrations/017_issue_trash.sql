-- Trash (docs/trash-plan.md): deleting an issue sets deleted_at; the sweeper deletes it for good after 30 days. The
-- table becomes issues_all and issues is a view of the live rows, so every query keeps reading and writing issues
-- (the view is auto-updatable: insert, update, delete, on conflict and for update work through it).
-- The view's select * froze its column list: a column added to issues_all later needs the view recreated
-- (create or replace view issues as select * from issues_all where deleted_at is null).
-- Constraints stay on the table, so a trashed issue keeps its number and rank; ranks are placed among issues_all.
alter table issues rename to issues_all;
alter table issues_all add column deleted_at timestamptz,
  add column deleted_by uuid references users(id) on delete set null;
create view issues as select * from issues_all where deleted_at is null;
create index issues_all_trash on issues_all (workspace_id, deleted_at) where deleted_at is not null;

-- issue.deleted goes out when an issue is trashed, by who trashed it; deleting a trashed one for good sends nothing more.
create or replace function webhook_issue_deleted() returns trigger language plpgsql as $$
declare
  pkey text;
begin
  select p.key into pkey from projects p
  where p.id = old.project_id and not p.private and exists (select 1 from webhooks w where w.workspace_id = old.workspace_id);
  if pkey is not null then
    perform webhook_enqueue(old.workspace_id, 'issue.deleted', jsonb_build_object(
      'actor', case when tg_op = 'UPDATE' then webhook_actor(new.deleted_by) end,
      'issue', jsonb_build_object('id', old.id, 'identifier', pkey || '-' || old.number, 'title', old.title)));
  end if;
  return null;
end $$;
drop trigger webhook_issue_deleted on issues_all;
create trigger webhook_issue_deleted after delete on issues_all for each row when (old.deleted_at is null)
  execute function webhook_issue_deleted();
create trigger webhook_issue_trashed after update of deleted_at on issues_all for each row
  when (old.deleted_at is null and new.deleted_at is not null) execute function webhook_issue_deleted();
