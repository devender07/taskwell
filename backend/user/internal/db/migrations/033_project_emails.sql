-- "Added to a project" gets its own email setting, split from assignments; existing rows keep their choice.
alter table notification_settings add column projects boolean default true not null;
update notification_settings set projects = assignments;
