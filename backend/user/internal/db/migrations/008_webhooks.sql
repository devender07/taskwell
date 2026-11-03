-- Outbound webhooks: a workspace's URLs that Tether POSTs signed JSON to when issues and comments change. Triggers
-- queue the deliveries, so every writer is covered; the user service's worker sends them.
create table webhooks (
  id           uuid primary key default uuidv7(),
  workspace_id uuid not null references workspaces on delete cascade,
  url          text not null check (char_length(url) <= 2000),
  secret       text not null,
  events       text[] not null check (cardinality(events) > 0),
  active       boolean not null default true,
  created_by   uuid references users on delete set null,
  created_at   timestamptz not null default now()
);
create index on webhooks (workspace_id);

create table webhook_deliveries (
  id             uuid primary key default uuidv7(),
  webhook_id     uuid not null references webhooks on delete cascade,
  event          text not null,
  payload        jsonb not null,
  status         text not null default 'pending' check (status in ('pending', 'delivered', 'failed')),
  attempts       int not null default 0,
  next_at        timestamptz not null default now(),
  response_code  int,
  response_error text,
  delivered_at   timestamptz,
  created_at     timestamptz not null default now()
);
create index on webhook_deliveries (next_at) where status = 'pending';
create index on webhook_deliveries (webhook_id, id desc);

-- webhook_enqueue queues the event for each active webhook of the workspace subscribed to it, or only for hook
-- (whatever its events: a ping), adding id, type, created_at and workspace to body, and wakes the workers.
create function webhook_enqueue(ws uuid, ev text, body jsonb, hook uuid default null) returns void
language plpgsql as $$
begin
  insert into webhook_deliveries (id, webhook_id, event, payload)
  select d.id, w.id, ev, body || jsonb_build_object('id', d.id, 'type', ev, 'workspace', s.slug,
    'created_at', to_char(now() at time zone 'utc', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'))
  from webhooks w join workspaces s on s.id = w.workspace_id, lateral (select uuidv7() as id) d
  where w.workspace_id = ws and (w.id = hook or (hook is null and w.active and ev = any(w.events)));
  if found then
    perform pg_notify('webhooks', '');
  end if;
end $$;

create function webhook_actor(id uuid) returns jsonb language sql stable as $$
  select jsonb_build_object('id', u.id, 'name', u.name) from users u where u.id = webhook_actor.id
$$;

-- Issues in private projects are never sent: a webhook is workspace-wide and its receiver isn't a member. An issue
-- deleted with its project or workspace isn't sent either (the project is gone by then).
-- ponytail: private projects are all-or-nothing; add a per-webhook opt-in if a customer asks.
create function webhook_issue_ref(issue uuid, ws out uuid, ref out jsonb) language sql stable as $$
  select i.workspace_id, jsonb_build_object('id', i.id, 'identifier', p.key || '-' || i.number, 'title', i.title)
  from issues i join projects p on p.id = i.project_id
  where i.id = issue and not p.private and exists (select 1 from webhooks w where w.workspace_id = i.workspace_id)
$$;

create function webhook_issue_event() returns trigger language plpgsql as $$
declare
  r record;
begin
  select * into r from webhook_issue_ref(new.issue_id);
  if r.ws is null then
    return null;
  end if;
  if new.kind = 'created' then
    perform webhook_enqueue(r.ws, 'issue.created', jsonb_build_object('issue', r.ref, 'actor', webhook_actor(new.actor_id)));
  else
    perform webhook_enqueue(r.ws, 'issue.updated', jsonb_build_object('issue', r.ref, 'actor', webhook_actor(new.actor_id),
      'change', jsonb_build_object('kind', new.kind, 'data', new.data)));
  end if;
  return null;
end $$;
create trigger webhook_issue_event after insert on issue_events for each row execute function webhook_issue_event();

create function webhook_comment() returns trigger language plpgsql as $$
declare
  r record;
begin
  select * into r from webhook_issue_ref(new.issue_id);
  if r.ws is not null then
    perform webhook_enqueue(r.ws, 'comment.created', jsonb_build_object('issue', r.ref, 'actor', webhook_actor(new.author_id),
      'comment', jsonb_build_object('id', new.id)));
  end if;
  return null;
end $$;
create trigger webhook_comment after insert on comments for each row execute function webhook_comment();

-- The deleting user isn't known here, so actor is null.
create function webhook_issue_deleted() returns trigger language plpgsql as $$
declare
  pkey text;
begin
  select p.key into pkey from projects p
  where p.id = old.project_id and not p.private and exists (select 1 from webhooks w where w.workspace_id = old.workspace_id);
  if pkey is not null then
    perform webhook_enqueue(old.workspace_id, 'issue.deleted', jsonb_build_object('actor', null,
      'issue', jsonb_build_object('id', old.id, 'identifier', pkey || '-' || old.number, 'title', old.title)));
  end if;
  return null;
end $$;
create trigger webhook_issue_deleted after delete on issues for each row execute function webhook_issue_deleted();
