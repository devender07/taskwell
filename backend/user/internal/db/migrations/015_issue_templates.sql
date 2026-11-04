-- Issue templates and recurring issues (docs/templates-plan.md): a template prefills a new issue; one that repeats
-- creates an issue from itself on next_on (UTC), then moves next_on on by its interval.
create table issue_templates (
  id           uuid primary key default uuidv7(),
  workspace_id uuid not null references workspaces(id) on delete cascade,
  name         text not null,
  title        text not null default '',
  description  jsonb,
  project_id   uuid references projects(id) on delete cascade,
  status_id    uuid references statuses(id) on delete set null,
  assignee_id  uuid references users(id) on delete set null,
  priority     smallint not null default 0,
  label_ids    uuid[] not null default '{}', -- deleted labels drop out when read
  repeat_every int check (repeat_every between 1 and 365),
  repeat_unit  text check (repeat_unit in ('day', 'week', 'month')),
  next_on      date,
  paused       boolean not null default false,
  created_by   uuid references users(id) on delete set null,
  created_at   timestamptz not null default now(),
  check ((repeat_every is null) = (repeat_unit is null) and (repeat_every is null) = (next_on is null))
);
create index issue_templates_workspace on issue_templates (workspace_id);
create index issue_templates_due on issue_templates (next_on) where repeat_every is not null and not paused;
