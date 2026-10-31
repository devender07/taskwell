-- Outbound email. Request handlers and the notification worker queue messages here and the mail worker sends them,
-- so nothing waits on SMTP. The worker claims a row by pushing next_attempt_at past its send timeout (a crashed
-- instance's claim expires), deletes it once sent, and on failure retries with backoff until it gives up: then the
-- row stays with failed_at and last_error.
create table outbound_mail (
  id              uuid primary key default uuidv7(),
  recipient       text not null,
  subject         text not null,
  body            text not null,
  attempts        int not null default 0,
  next_attempt_at timestamptz not null default now(),
  last_error      text,
  failed_at       timestamptz,
  created_at      timestamptz not null default now()
);
create index on outbound_mail (next_attempt_at) where failed_at is null;
