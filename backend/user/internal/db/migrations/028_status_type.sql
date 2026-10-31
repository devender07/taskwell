-- A status's type is a free label shown in Settings → Workflow ("Reopen", "In review"); null shows its category's name.
-- The category stays the behaviour: icons, what counts as open or done.
alter table statuses add column type text check (char_length(type) between 1 and 30);
