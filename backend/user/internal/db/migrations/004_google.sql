-- Google sign-in. Users found by google_sub first, then linked by verified email.
alter table users add column google_sub text unique;

insert into system_config (key, value, secret, description) values
  ('google_client_id',     '', false, 'Google OAuth client ID; empty disables "Continue with Google". Authorized redirect URI: {app_base_url}/api/auth/google/callback'),
  ('google_client_secret', '', true,  'Google OAuth client secret');
