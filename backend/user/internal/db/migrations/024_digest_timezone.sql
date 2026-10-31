-- Each user's daily digest goes out at digest_hour_utc in their own timezone (an IANA name), not in UTC.
alter table notification_settings add column timezone text not null default 'UTC';

update system_config set description = 'Hour (0-23) daily notification digests are sent, in each user''s timezone'
where key = 'digest_hour_utc';
