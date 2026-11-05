-- Notes (docs/notes-plan.md): Keep-style notes, shared by their owner with other members. They replace the private
-- "Notes" chat (kind 'self'), whose messages move here.
create table notes (
  id           uuid primary key default uuidv7(),
  workspace_id uuid not null references workspaces(id) on delete cascade,
  owner_id     uuid not null references users(id) on delete cascade,
  title        text not null default '',
  body         jsonb,
  color        text not null default '' check (color = '' or color ~ '^#[0-9a-f]{6}$'),
  created_at   timestamptz not null default now(),
  updated_at   timestamptz not null default now(),
  updated_by   uuid references users(id) on delete set null
);
create index notes_workspace on notes (workspace_id);

-- Everyone who has the note, the owner included. Pin, archive and labels are each person's own.
create table note_members (
  note_id  uuid not null references notes(id) on delete cascade,
  user_id  uuid not null references users(id) on delete cascade,
  pinned   boolean not null default false,
  archived boolean not null default false,
  labels   text[] not null default '{}',
  added_at timestamptz not null default now(),
  primary key (note_id, user_id)
);
create index note_members_user on note_members (user_id);

with moved as (
  insert into notes (workspace_id, owner_id, body, created_at, updated_at, updated_by)
  select c.workspace_id, c.created_by, m.body, m.created_at, coalesce(m.edited_at, m.created_at), c.created_by
  from chats c join messages m on m.chat_id = c.id
  where c.kind = 'self' and c.created_by is not null and m.parent_id is null and m.deleted_at is null and m.body is not null
  returning id, owner_id
)
insert into note_members (note_id, user_id) select id, owner_id from moved;

-- Messages, their replies, reactions, members and notifications cascade.
delete from chats where kind = 'self';
