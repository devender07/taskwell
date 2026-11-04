# S3 storage plan

Scope: uploads (attachments, avatars, AI test screenshots) move from the local `UPLOAD_DIR` disk to S3 or any
S3-compatible store (MinIO, Cloudflare R2). Every storage setting comes from `system_config`, so an admin changes it in
the admin app's Config page without a restart. `DATABASE_URL` stays an env var: `system_config` lives in that database.

S3 only: there is no local-disk fallback. Dev runs SeaweedFS (S3 API) in `docker-compose.yml`.

Out of scope: copying existing files from disk to the bucket (one `aws s3 sync <UPLOAD_DIR> s3://<bucket>` does it,
since object keys are the same file ids), per-workspace buckets, CDN.

## `system_config` keys (migration `009_s3_storage.sql`)

| Key | Default | Secret | Meaning |
|---|---|---|---|
| `s3_endpoint` | `http://localhost:9000` | no | S3-compatible endpoint; empty uses AWS. Set means path-style URLs |
| `s3_region` | `us-east-1` | no | Bucket region (`auto` for R2) |
| `s3_bucket` | `taskwell` | no | Bucket name; empty makes uploads fail |
| `s3_access_key_id` | `taskwell` | no | Access key |
| `s3_secret_access_key` | `taskwell-secret` | yes | Secret key |

The defaults match the dev S3 store, like `smtp_host` matches Mailpit.

## Storage

- `user/internal/storage`: `Put`, `Delete`, `URL` (presigned GET) and `Old` (list objects older than a cutoff, for the
  sweep). It rebuilds its S3 client when the settings change. Object key = file uuid.
- Upload streams to a temp file (`os.CreateTemp`), then `PutObject` inside the row's transaction, so a row only
  commits once its bytes are stored.
- AI test screenshots `PutObject` the JPEG in `addStep`'s transaction.
- `Content-Type`, `Content-Disposition` and `Cache-Control` are stored on the object at upload (some S3-compatible
  stores ignore presign response overrides).
- The sweep deletes objects for deleted rows and lists the bucket for objects older than 24 hours without a row.

## API contract

No change to paths or JSON. `GET /api/files/{id}/{name}` still checks access, then answers `302` to a presigned URL
(valid 1 hour); the object answers with its stored headers (images inline, others as a download). The redirect itself
is cacheable for 50 minutes.

## Deploy

`UPLOAD_DIR` and the `uploads` volume go away. The bucket must be private; the browser and the MCP server must reach
`s3_endpoint`. Caddy's CSP allows images and media from the new `S3_ORIGIN` env var.
