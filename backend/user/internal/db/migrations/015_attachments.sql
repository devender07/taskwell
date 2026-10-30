-- Uploaded files (attachments in descriptions and comments, and avatars). The bytes live on disk at <UPLOAD_DIR>/<id>.
-- ponytail: deleting a workspace drops its rows but leaves the files on disk; add an orphan sweep when disk use matters.
create table files (
  id           uuid primary key default uuidv7(),
  workspace_id uuid references workspaces on delete cascade, -- null for avatars
  uploader_id  uuid references users on delete set null,
  name         text not null,
  content_type text not null,
  size         bigint not null,
  created_at   timestamptz not null default now()
);
create index on files (workspace_id);

alter table users add column avatar_file_id uuid references files on delete set null;

insert into system_config (key, value, secret, description) values
  ('upload_max_mb', '10', false, 'Largest attachment upload in MB');
