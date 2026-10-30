-- Notifications: who watches which issue, each user's notifications (the inbox, and the email queue via emailed_at),
-- and their email settings (a missing row means the defaults).
create table issue_watchers (
  issue_id   uuid not null references issues on delete cascade,
  user_id    uuid not null references users on delete cascade,
  created_at timestamptz not null default now(),
  primary key (issue_id, user_id)
);
create index on issue_watchers (user_id);

create table notifications (
  id           uuid primary key default uuidv7(),
  user_id      uuid not null references users on delete cascade,
  workspace_id uuid not null references workspaces on delete cascade,
  issue_id     uuid not null references issues on delete cascade,
  actor_id     uuid references users on delete set null,
  kind         text not null check (kind in ('mention', 'assigned', 'status', 'comment')),
  data         jsonb not null default '{}', -- status: {from, to}; comment and mention: {comment_id, excerpt} or {}
  read_at      timestamptz,
  emailed_at   timestamptz, -- sent, put in a digest, or skipped by the user's settings
  created_at   timestamptz not null default now()
);
create index on notifications (user_id, workspace_id, id desc);
create index on notifications (issue_id);
create index on notifications (created_at) where emailed_at is null;

create table notification_settings (
  user_id        uuid primary key references users on delete cascade,
  email          text not null default 'instant' check (email in ('instant', 'digest', 'off')),
  mentions       boolean not null default true,
  assignments    boolean not null default true,
  status         boolean not null default true,
  comments       boolean not null default true,
  last_digest_at timestamptz
);

insert into issue_watchers (issue_id, user_id)
select id, created_by from issues where created_by is not null
union
select id, assignee_id from issues where assignee_id is not null;

insert into system_config (key, value, secret, description) values
  ('digest_hour_utc', '8', false, 'Hour (UTC, 0-23) daily notification digests are sent');
