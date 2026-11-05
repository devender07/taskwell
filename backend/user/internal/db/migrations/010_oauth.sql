-- OAuth for MCP clients (docs/mcp-oauth-plan.md): clients register themselves, and an authorization code is traded
-- once, with its PKCE verifier, for a personal API token. The sweep deletes expired codes and idle clients.
create table oauth_clients (
  id            uuid primary key default gen_random_uuid(),
  name          text not null check (char_length(name) between 1 and 100),
  redirect_uris text[] not null check (cardinality(redirect_uris) between 1 and 5),
  created_at    timestamptz not null default now()
);

create table oauth_codes (
  code_hash      bytea primary key,
  client_id      uuid not null references oauth_clients on delete cascade,
  user_id        uuid not null references users on delete cascade,
  redirect_uri   text not null,
  code_challenge text not null,
  expires_at     timestamptz not null
);
create index oauth_codes_client_id on oauth_codes (client_id);
