-- P1 security hardening: invite caps, upload quota, password-set codes, and admin sessions, MFA and audit log.

-- Invite emails sent (new invites and resends), counted per inviter and per workspace over a day, so the caps hold
-- across every API instance. Rows older than a day are dropped as new ones arrive.
create table invite_sends (
  workspace_id uuid not null references workspaces on delete cascade,
  user_id      uuid not null references users on delete cascade,
  at           timestamptz not null default now()
);
create index on invite_sends (user_id, at);
create index on invite_sends (workspace_id, at);
create index on invite_sends (at);

insert into system_config (key, value, secret, description) values
  ('invite_daily_user_limit', '50', false, 'Invite emails one person can send per day'),
  ('invite_daily_workspace_limit', '200', false, 'Invite emails one workspace can send per day'),
  ('upload_quota_mb', '5120', false, 'Total attachment storage per workspace in MB');

-- An emailed code proves the owner is present before a first password is added to an account that has none.
alter table auth_tokens drop constraint auth_tokens_purpose_check;
alter table auth_tokens add constraint auth_tokens_purpose_check
  check (purpose in ('signin_code', 'password_reset', 'email_verify', 'password_set'));

-- Admin sign-ins. The cookie's token names a row; signing out or suspension ends it at once. A row with mfa_at null
-- has passed the password only and can do nothing but finish the second factor.
create table admin_sessions (
  id           uuid primary key default uuidv7(),
  user_id      uuid not null references users on delete cascade,
  mfa_at       timestamptz,
  created_at   timestamptz not null default now(),
  last_seen_at timestamptz not null default now(),
  expires_at   timestamptz not null
);
create index on admin_sessions (user_id);
create index on admin_sessions (expires_at);

-- Platform admins' TOTP (RFC 6238). enabled_at is null until the first code confirms enrolment. last_step stops a
-- code being replayed. Recovery codes are stored as SHA-256 hex and removed when used.
create table admin_mfa (
  user_id        uuid primary key references users on delete cascade,
  secret         text not null,
  enabled_at     timestamptz,
  last_step      bigint not null default 0,
  recovery_codes text[] not null default '{}'
);

-- Append-only record of admin actions. actor_id has no foreign key so rows outlive their users; config changes
-- record the key, never the value.
create table admin_audit (
  id          uuid primary key default uuidv7(),
  at          timestamptz not null default now(),
  actor_id    uuid,
  actor_email text not null default '',
  action      text not null,
  target      text not null default '',
  ip          text not null default ''
);

create function admin_audit_append_only() returns trigger language plpgsql as $$
begin
  raise exception 'admin_audit is append-only';
end $$;
create trigger admin_audit_no_change before update or delete on admin_audit
  for each row execute function admin_audit_append_only();
create trigger admin_audit_no_truncate before truncate on admin_audit
  for each statement execute function admin_audit_append_only();
