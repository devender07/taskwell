-- Workspace invites. Accepting deletes the row; only the token's SHA-256 is stored.
create table workspace_invites (
  id           uuid primary key default uuidv7(),
  workspace_id uuid not null references workspaces on delete cascade,
  email        text not null,
  token_hash   bytea not null unique,
  invited_by   uuid references users on delete set null,
  expires_at   timestamptz not null,
  created_at   timestamptz not null default now(),
  unique (workspace_id, email)
);

insert into system_config (key, value, secret, description) values
  ('invite_ttl_days', '7', false, 'Workspace invite link lifetime in days');
