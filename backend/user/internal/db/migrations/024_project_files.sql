-- Project files (docs/project-files-plan.md): a project's own files in nested folders (listed), and the attachments
-- uploaded from its issues and comments (not listed). The sweep leaves listed files alone: no document references them.
create table file_folders (
  id uuid primary key default uuidv7(),
  project_id uuid not null references projects(id) on delete cascade,
  parent_id uuid references file_folders(id) on delete cascade,
  name text not null check (length(name) between 1 and 100),
  created_by uuid references users(id) on delete set null,
  created_at timestamptz not null default now()
);
create unique index file_folders_name on file_folders (project_id, parent_id, lower(name)) nulls not distinct;

alter table files
  add column project_id uuid references projects(id) on delete cascade,
  add column folder_id uuid references file_folders(id) on delete restrict,
  add column listed boolean not null default false;
create index on files (project_id, folder_id) where project_id is not null;
create index on files (folder_id) where folder_id is not null;
