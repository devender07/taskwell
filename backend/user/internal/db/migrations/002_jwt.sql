-- Sessions become rotating refresh tokens; short-lived JWT access tokens are not stored.
delete from sessions;
alter table sessions rename to refresh_tokens;
alter table refresh_tokens
  add column family_id uuid not null, -- every token rotated from one sign-in shares a family
  add column used_at   timestamptz;   -- set on rotation; presenting a used token revokes the family
create index on refresh_tokens (family_id);

update system_config
   set key = 'refresh_ttl_hours',
       description = 'Refresh token lifetime in hours; each refresh restarts it, so this is how long an idle sign-in lasts'
 where key = 'session_ttl_hours';

insert into system_config (key, value, secret, description) values
  ('access_ttl_minutes', '15', false, 'JWT access token lifetime in minutes'),
  ('jwt_secret',         '',   true,  'HMAC key that signs access tokens; generated on first start if empty, changing it signs everyone out of their access tokens');
