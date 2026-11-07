-- A presigned upload the browser hasn't finished yet. The object key is the id; finishing moves it to files.
create table uploads (
    id uuid default uuidv7() primary key,
    workspace_id uuid references workspaces (id) on delete cascade, -- null: an avatar
    user_id uuid not null references users (id) on delete cascade,
    name text not null,
    content_type text not null,
    size bigint not null,
    created_at timestamptz default now() not null
);
create index uploads_created_at_idx on uploads (created_at);

insert into system_config (key, value, secret, description) values
  ('files_cdn_url', '', false, 'Where the file Worker serves uploads (https://files.taskwell.dev); empty redirects to presigned bucket links'),
  ('files_cdn_secret', '', true, 'Signs file links for the Worker; the same value as its FILES_SECRET')
on conflict (key) do nothing;
