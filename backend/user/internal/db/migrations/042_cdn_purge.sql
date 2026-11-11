-- Deleting a file also purges it from Cloudflare's edge cache (storage.purge), once these are set.
insert into system_config (key, value, secret, description) values
  ('files_cdn_zone_id', '', false, 'Cloudflare zone id of the file CDN''s domain (taskwell.dev), for purging deleted files from the edge cache; empty skips the purge'),
  ('files_cdn_api_token', '', true, 'Cloudflare API token with the Cache Purge permission on that zone')
on conflict (key) do nothing;
