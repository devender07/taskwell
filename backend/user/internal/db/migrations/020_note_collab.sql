-- Notes: live collaboration and share permissions (docs/notes-collab-plan.md). The owner's row is always 'edit'.
alter table note_members add column role text not null default 'edit' check (role in ('edit', 'view'));

-- The live body's state, like issues': the seed claim and the count of REST body writes (sent in the ready frame).
alter table notes add column seed_claimed_at timestamptz, add column body_generation integer not null default 0;

-- The live body's stored Yjs updates, like issue_doc_updates (same columns and compaction rules).
create table note_doc_updates (
  id      bigint generated always as identity primary key,
  note_id uuid not null references notes(id) on delete cascade,
  data    bytea not null
);
create index note_doc_updates_note_id_id_idx on note_doc_updates (note_id, id);
