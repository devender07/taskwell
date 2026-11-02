-- The workspace name a sign-up chose, kept with its code until verifying creates the workspace; null = named after them.
alter table auth_tokens add column workspace_name text;
