-- Invite prompt (docs/invite-prompt-plan.md): sign-up no longer joins invited workspaces. Pending invites are offered
-- in a prompt; "Not now" sets dismissed_at and leaves an 'invite' notification (no issue, chat or project) that can
-- still join. data keeps the invite's id and workspace name for when the invite is gone.
alter table workspace_invites add column dismissed_at timestamptz;
alter table notifications add column invite_id uuid references workspace_invites(id) on delete set null;
create index on notifications (invite_id) where invite_id is not null;
alter table notifications drop constraint notifications_kind_check,
  add constraint notifications_kind_check
    check (kind in ('mention', 'assigned', 'status', 'comment', 'chat_mention', 'message', 'project_added', 'invite'));
alter table notifications drop constraint notifications_target_check,
  add constraint notifications_target_check
    check (num_nonnulls(issue_id, chat_id, project_id) = case when kind = 'invite' then 0 else 1 end);
