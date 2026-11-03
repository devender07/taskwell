-- Uploads live in S3 or an S3-compatible store. The defaults match the dev S3 store in docker-compose.yml.
insert into system_config (key, value, secret, description) values
  ('s3_endpoint', 'http://localhost:9000', false, 'S3-compatible endpoint (MinIO, R2); empty uses AWS. Browsers follow presigned links to it'),
  ('s3_region', 'us-east-1', false, 'Bucket region (auto for R2)'),
  ('s3_bucket', 'tether', false, 'Private bucket that holds uploads; empty makes uploads fail'),
  ('s3_access_key_id', 'tether', false, 'S3 access key ID'),
  ('s3_secret_access_key', 'tether-secret', true, 'S3 secret access key')
on conflict (key) do nothing;
