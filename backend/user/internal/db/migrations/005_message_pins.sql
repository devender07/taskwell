-- A chat message pinned to the top of its chat until pinned_until.
alter table messages add column pinned_until timestamptz;
create index messages_pinned_idx on messages (chat_id) where pinned_until is not null;
