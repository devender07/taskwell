-- Chat: the workspace chat everyone is in, each member's notes to self, direct messages and private groups. Messages
-- are rich-text docs with one level of threaded replies and emoji reactions. Chat mentions and direct messages become
-- notifications, so notifications no longer always belong to an issue.
create table chats (
  id              uuid primary key default uuidv7(),
  workspace_id    uuid not null references workspaces on delete cascade,
  kind            text not null check (kind in ('workspace', 'self', 'direct', 'group')),
  name            text,                    -- groups only
  pair            text,                    -- direct: '<smaller user id>:<larger user id>'; self: '<user id>'
  created_by      uuid references users on delete set null,
  last_message_at timestamptz,             -- the newest message or reply
  created_at      timestamptz not null default now()
);
create unique index on chats (workspace_id) where kind = 'workspace';
create unique index on chats (workspace_id, pair) where kind in ('direct', 'self');
create index on chats (created_by);

create table chat_members (
  chat_id      uuid not null references chats on delete cascade,
  user_id      uuid not null references users on delete cascade,
  last_read_id uuid,                       -- newest message (top level or reply) the user has read; null = none
  joined_at    timestamptz not null default now(),
  primary key (chat_id, user_id)
);
create index on chat_members (user_id);

create table messages (
  id            uuid primary key default uuidv7(),
  chat_id       uuid not null references chats on delete cascade,
  parent_id     uuid references messages on delete cascade,   -- thread root; replies can't have replies
  author_id     uuid references users on delete set null,
  body          jsonb,                     -- ProseMirror doc; null once deleted
  mentions      uuid[] not null default '{}', -- the people mentioned who can see the chat, for unread mention counts
  created_at    timestamptz not null default now(),
  edited_at     timestamptz,
  deleted_at    timestamptz,
  reply_count   int not null default 0,    -- replies not deleted
  last_reply_at timestamptz
);
create index on messages (chat_id, id);
create index on messages (parent_id, id) where parent_id is not null;
create index on messages (author_id);

create table message_reactions (
  message_id uuid not null references messages on delete cascade,
  user_id    uuid not null references users on delete cascade,
  emoji      text not null check (char_length(emoji) between 1 and 16),
  created_at timestamptz not null default now(),
  primary key (message_id, user_id, emoji)
);
create index on message_reactions (user_id);

-- chat_name is the chat's name as the viewer sees it: a direct chat is named after the other person.
create function chat_name(c chats, viewer uuid) returns text language sql stable as $$
  select case c.kind
    when 'workspace' then (select name from workspaces where id = c.workspace_id)
    when 'self' then 'Notes'
    when 'group' then c.name
    else coalesce((select u.name from chat_members o join users u on u.id = o.user_id
                   where o.chat_id = c.id and o.user_id <> viewer order by o.joined_at limit 1), 'Someone')
  end
$$;

-- notifications: chat kinds; issue becomes optional
alter table notifications alter column issue_id drop not null;
alter table notifications add column chat_id uuid references chats on delete cascade;
alter table notifications add column message_id uuid references messages on delete cascade;
alter table notifications drop constraint notifications_kind_check;
alter table notifications add constraint notifications_kind_check
  check (kind in ('mention', 'assigned', 'status', 'comment', 'chat_mention', 'message'));
alter table notifications add constraint notifications_target_check
  check ((issue_id is not null) <> (chat_id is not null));
create index on notifications (chat_id);
create index on notifications (message_id);
alter table notification_settings add column chat boolean not null default true;

-- Every workspace has its workspace chat; new workspaces get theirs, and members their notes, on first GET chats.
insert into chats (workspace_id, kind) select id, 'workspace' from workspaces;
