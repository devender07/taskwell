-- Compliance (docs/compliance-plan.md): terms acceptance at sign-up, the account security log, account deletion codes
-- and an append-only workspace audit log.
alter table users add column terms_accepted_at timestamptz; -- null for accounts made before this

-- What happened to an account (sign-ins, password, email, two-factor, API tokens), kept a year (sweep).
-- uuidv7 ids so GET /api/me/account-events can page by id, newest first.
create table account_events (
    id uuid primary key default uuidv7(),
    user_id uuid not null references users (id) on delete cascade,
    action text not null check (action in ('sign_in', 'sign_in_failed', 'sign_out', 'password_changed', 'password_added',
        'email_changed', 'mfa_enabled', 'mfa_disabled', 'token_created', 'token_deleted')),
    ip inet,
    user_agent text not null default '',
    detail text not null default '',
    at timestamptz not null default now()
);
create index account_events_user_idx on account_events (user_id, id desc);

alter table auth_tokens drop constraint auth_tokens_purpose_check,
  add constraint auth_tokens_purpose_check check (purpose in
    ('signin_code', 'password_reset', 'email_verify', 'password_set', 'email_change_old', 'email_change_new', 'account_delete'));

-- workspace_audit is append-only, except what its foreign keys and the sweep need: deleting a user nulls actor_id
-- (and nothing else), deleting a workspace cascades to its rows, and rows older than a year are swept.
create function workspace_audit_append_only() returns trigger language plpgsql as $$
begin
  if tg_op = 'UPDATE' then
    if old.actor_id is not null and new.actor_id is null and (to_jsonb(new) - 'actor_id') = (to_jsonb(old) - 'actor_id') then
      return new;
    end if;
  elsif tg_op = 'DELETE' then
    if old.at < now() - interval '1 year' or not exists (select 1 from workspaces where id = old.workspace_id) then
      return old;
    end if;
  end if;
  raise exception 'workspace_audit is append-only';
end $$;
create trigger workspace_audit_no_change before update or delete on workspace_audit
    for each row execute function workspace_audit_append_only();
create trigger workspace_audit_no_truncate before truncate on workspace_audit
    for each statement execute function workspace_audit_append_only();
