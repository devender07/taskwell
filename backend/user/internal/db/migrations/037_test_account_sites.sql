-- A test account can belong to one test site: its password is typed only on pages of that site's host (null: any
-- of the workspace's sites, as before). Removing the site removes its accounts.
alter table test_accounts add column site_id uuid references test_sites on delete cascade;
create index on test_accounts (site_id);
