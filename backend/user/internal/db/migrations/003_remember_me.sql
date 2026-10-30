-- "Remember me": persistent cookies for refresh_ttl_hours, otherwise browser-session cookies capped at short_refresh_ttl_hours.
-- Both lifetimes count from sign-in; refreshing rotates the token but keeps the family's expiry.
alter table refresh_tokens add column remember boolean not null default true;

update system_config
   set description = 'Sign-in lifetime in hours with "Remember me", counted from sign-in'
 where key = 'refresh_ttl_hours';

insert into system_config (key, value, secret, description) values
  ('short_refresh_ttl_hours', '24', false, 'Sign-in lifetime in hours without "Remember me"; the cookies also end when the browser closes');
