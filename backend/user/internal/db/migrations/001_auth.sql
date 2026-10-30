create table system_config (
  key         text primary key,
  value       text not null default '',
  secret      boolean not null default false,
  description text not null default '',
  updated_at  timestamptz not null default now()
);

insert into system_config (key, value, secret, description) values
  ('smtp_host',         'localhost',                      false, 'SMTP server host; empty disables email'),
  ('smtp_port',         '1025',                           false, 'SMTP server port'),
  ('smtp_username',     '',                               false, 'SMTP username; empty skips auth'),
  ('smtp_password',     '',                               true,  'SMTP password'),
  ('smtp_from',         'Tether <no-reply@tether.local>', false, 'From header for outgoing email'),
  ('app_base_url',      'http://localhost:5173',          false, 'Public URL of the user app, used in email links'),
  ('session_ttl_hours', '720',                            false, 'Session lifetime in hours'),
  ('otp_ttl_minutes',   '10',                             false, 'Sign-in code lifetime in minutes'),
  ('reset_ttl_minutes', '30',                             false, 'Password reset link lifetime in minutes');

create table users (
  id            uuid primary key default uuidv7(),
  name          text not null,
  email         text not null unique,
  password_hash text,
  created_at    timestamptz not null default now()
);

create table sessions (
  token_hash bytea primary key,
  user_id    uuid not null references users on delete cascade,
  expires_at timestamptz not null,
  created_at timestamptz not null default now()
);
create index on sessions (user_id);

create table auth_tokens (
  id         uuid primary key default uuidv7(),
  user_id    uuid not null references users on delete cascade,
  purpose    text not null check (purpose in ('signin_code', 'password_reset')),
  token_hash bytea not null,
  expires_at timestamptz not null,
  attempts   int not null default 0,
  used_at    timestamptz,
  created_at timestamptz not null default now()
);
create index on auth_tokens (user_id, purpose) where used_at is null;
create index on auth_tokens (token_hash);
