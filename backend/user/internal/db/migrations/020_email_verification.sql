-- Email verification. A sign-up creates the user unverified and without a password; the emailed code (bound to the
-- signing-up browser) carries the name and password hash to apply once it is verified. Accounts that existed before
-- this migration count as verified.
alter table users add column email_verified_at timestamptz;
update users set email_verified_at = created_at;

alter table auth_tokens drop constraint auth_tokens_purpose_check;
alter table auth_tokens add constraint auth_tokens_purpose_check
  check (purpose in ('signin_code', 'password_reset', 'email_verify'));
alter table auth_tokens
  add column name          text, -- email_verify only
  add column password_hash text; -- email_verify only

-- Failed passwords and codes, counted per key (e.g. "code:ann@x.com") over a sliding window, so brute force is capped
-- across every API instance.
create table auth_failures (
  key text not null,
  at  timestamptz not null default now()
);
create index on auth_failures (key, at);
create index on auth_failures (at);
