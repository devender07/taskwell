-- Rate limits every API instance shares (user and admin services): a token bucket per key ("ip:<addr>",
-- "send:<email>", "admin-ip:<addr>"). ok is whether the latest take spent a token. Unlogged: a crash empties it,
-- which only refills the buckets. The sweep drops rows untouched for an hour (every bucket is full by then).
create unlogged table rate_limits (
  key    text primary key,
  tokens double precision not null,
  at     timestamptz not null,
  ok     boolean not null
);
create index on rate_limits (at);

-- Open realtime sockets, so the per-user cap holds across instances. Like issue_viewers: each instance refreshes
-- seen_at every 30s, rows past 90s are a crashed instance's and don't count.
create unlogged table realtime_conns (
  conn_id  text primary key,
  user_id  uuid not null references users on delete cascade,
  instance text not null,
  seen_at  timestamptz not null default now()
);
create index on realtime_conns (user_id);
create index on realtime_conns (instance);

-- Admin MFA is enrolled only from the CLI (tether-admin -promote / -reset-mfa), which enables it at once. Drop web
-- enrolments that were never confirmed: those admins need the CLI step. The admin service encrypts secrets at rest
-- ("v1:" + base64 AES-GCM) and converts plaintext ones when it starts.
delete from admin_mfa where enabled_at is null;

-- Bumped by every REST write of an issue's description, so an offline live editor can tell its document was replaced.
alter table issues add column description_generation int not null default 0;

-- null is "never chosen" (the digest uses UTC). Rows set to UTC on purpose can't be told apart from the old default,
-- so every UTC row becomes null: those users are offered their browser's zone once more.
alter table notification_settings alter column timezone drop not null, alter column timezone drop default;
update notification_settings set timezone = null where timezone = 'UTC';
