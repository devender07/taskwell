-- Who changed the workspace itself: members, roles, invites, projects, integrations, webhooks, permanent deletes.
-- The actor's name and email are copied so an entry reads right after the user is gone.
create table workspace_audit (
    id uuid default uuidv7() primary key,
    workspace_id uuid not null references workspaces (id) on delete cascade,
    actor_id uuid references users (id) on delete set null,
    actor_name text not null default '',
    actor_email text not null default '',
    action text not null,
    target text not null default '',
    detail jsonb not null default '{}',
    ip text not null default '',
    at timestamptz default now() not null
);
create index workspace_audit_workspace_idx on workspace_audit (workspace_id, id desc);
