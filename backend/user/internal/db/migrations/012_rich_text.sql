-- descriptions become ProseMirror JSON; existing plain text becomes one paragraph per line
alter table issues add column description_doc jsonb;
update issues set description_doc = jsonb_build_object('type', 'doc', 'content', (
  select jsonb_agg(case when line = '' then jsonb_build_object('type', 'paragraph')
    else jsonb_build_object('type', 'paragraph', 'content', jsonb_build_array(jsonb_build_object('type', 'text', 'text', line))) end order by n)
  from regexp_split_to_table(description, E'\r?\n') with ordinality as t(line, n)))
where description <> '';
alter table issues drop column description;
alter table issues rename column description_doc to description;

-- Yjs updates for a description being edited live; the client folds them into one row now and then
create table issue_doc_updates (
  id       bigint generated always as identity primary key,
  issue_id uuid not null references issues on delete cascade,
  data     bytea not null
);
create index on issue_doc_updates (issue_id, id);
