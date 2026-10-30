-- Old project keys keep resolving to their project, so ENG-142 links survive a key change.
create table project_key_aliases (
  workspace_id uuid not null references workspaces on delete cascade,
  key          text not null,
  project_id   uuid not null references projects on delete cascade,
  primary key (workspace_id, key)
);

-- A current key wins; otherwise an alias; otherwise the key unchanged (so lookups simply find nothing).
create function canonical_key(ws uuid, k text) returns text language sql stable as $$
  select coalesce(
    (select p.key from projects p where p.workspace_id = ws and p.key = k),
    (select p.key from project_key_aliases a join projects p on p.id = a.project_id where a.workspace_id = ws and a.key = k),
    k)
$$;

-- Changing a key keeps the old one as an alias; a key taken by any project stops being an alias.
create function project_key_alias() returns trigger language plpgsql as $$
begin
  delete from project_key_aliases where workspace_id = new.workspace_id and key = new.key;
  if tg_op = 'UPDATE' and old.key <> new.key then
    insert into project_key_aliases (workspace_id, key, project_id) values (old.workspace_id, old.key, old.id)
    on conflict (workspace_id, key) do update set project_id = excluded.project_id;
  end if;
  return new;
end
$$;

create trigger project_key_alias after insert or update of key on projects
  for each row execute function project_key_alias();
