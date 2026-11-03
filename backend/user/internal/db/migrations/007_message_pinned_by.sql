-- Who pinned a message: only they can unpin or re-pin it while the pin lasts.
alter table messages add column pinned_by uuid references users on delete set null;
