-- "Delete for me" on a direct chat: the member no longer sees messages with an id below cleared_id.
alter table chat_members add column cleared_id uuid;
