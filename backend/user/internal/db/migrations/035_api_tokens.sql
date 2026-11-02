-- Personal API tokens: a Bearer tth_… request signs in as the token's user (docs/mcp-plan.md).
create table api_tokens (
  id           uuid primary key default uuidv7(),
  user_id      uuid not null references users on delete cascade,
  name         text not null check (char_length(name) between 1 and 60),
  token_hash   bytea not null unique,           -- sha256 of the full token
  last_used_at timestamptz,
  created_at   timestamptz not null default now()
);
create index on api_tokens (user_id);
