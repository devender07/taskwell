-- Re-inviting someone keeps the link they already have working: the previous token stays valid until the invite
-- expires. "Resend" (renew) still replaces both.
alter table workspace_invites add column prev_token_hash bytea;

-- A project key someone else used is theirs for good: old links (KEY-12 in PRs, chat, emails) must never start
-- pointing at another project's issues. Taking back your own old key still works.
create or replace function project_key_alias() returns trigger language plpgsql as $$
begin
  if exists (select 1 from project_key_aliases where workspace_id = new.workspace_id and key = new.key and project_id <> new.id) then
    raise exception 'project key % was used by another project', new.key using errcode = 'unique_violation';
  end if;
  delete from project_key_aliases where workspace_id = new.workspace_id and key = new.key;
  if tg_op = 'UPDATE' and old.key <> new.key then
    insert into project_key_aliases (workspace_id, key, project_id) values (old.workspace_id, old.key, old.id)
    on conflict (workspace_id, key) do update set project_id = excluded.project_id;
  end if;
  return new;
end
$$;

create index on workspace_invites (prev_token_hash) where prev_token_hash is not null;
