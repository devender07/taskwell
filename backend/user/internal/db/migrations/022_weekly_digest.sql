-- Weekly digest (docs/weekly-digest-plan.md): a Monday email of the person's closed, due and blocked issues.
-- last_weekly_at defaults to now so existing and new rows wait for the next Monday instead of getting one at once.
alter table notification_settings
  add column weekly boolean not null default true,
  add column last_weekly_at timestamptz default now();
