-- Tether staff who can use the admin app (promoted with `tether-admin -promote email`), and account suspension.
alter table users add column platform_admin boolean not null default false;
alter table users add column suspended_at timestamptz;
