-- Two-factor sign-in for users (docs/two-factor-plan.md): an authenticator-app (TOTP) secret, sealed like admin_mfa's
-- (shared/totp), the last step used so a code can't be replayed, and single-use recovery code hashes. enabled_at is
-- null while setting up.
create table user_mfa (
  user_id        uuid primary key references users(id) on delete cascade,
  secret         text not null,
  last_step      bigint not null default 0,
  recovery_codes text[] not null,
  enabled_at     timestamptz,
  created_at     timestamptz not null default now()
);
