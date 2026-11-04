# Two-factor sign-in plan (users)

Scope: users can turn on an authenticator-app second step (TOTP), with single-use recovery codes. Once it's on, every sign-in method asks for it: password, email code and Google. It reuses the admin app's TOTP code.

Out of scope:
- Requiring two-factor for a whole workspace. `ponytail:` add a workspace setting when a customer asks.
- WebAuthn and passkeys.
- SMS.
- Remembering a device.

## Shared code

Move `backend/admin/internal/auth/totp.go` (and the sealing helpers it needs from `seal.go`) into the shared module, `backend/shared/totp/`, with exported names. The admin app then imports it, and its tests move with it. The user app gets the sealing key the way the admin app does (see `admin/cmd/admin/main.go` `MFAKey`). In dev it falls back like the other keys. In production it is required (`MFA_SECRET_KEY`, or whatever the admin app reads; both apps read the same variable).

## Schema (`016_user_mfa.sql`)

```sql
create table user_mfa (
  user_id uuid primary key references users(id) on delete cascade,
  secret text not null,              -- sealed
  last_step bigint not null default 0,
  recovery_codes text[] not null,    -- hashes
  enabled_at timestamptz,            -- null while setting up
  created_at timestamptz not null default now()
);
```

The pending sign-in lives in a short-lived signed cookie, `taskwell_mfa` (5 min, HttpOnly, path `/api/auth`). It holds the user id, the remember flag and an expiry, signed with the JWT secret the same way the existing tokens are.

## API contract

| Method & path | Needs | Success | Failures |
|---|---|---|---|
| `GET /api/me/mfa` | signed in | `200 {"enabled": bool, "recovery_left": n}` | 401 |
| `POST /api/me/mfa/setup` | signed in, `password` (or an `email_code` from `POST /api/me/password/code` when the account has no password, matching how password change works) | `200 {"secret": "BASE32", "otpauth_url": "otpauth://…"}`; starts or restarts setup | 400 `password` "Wrong password.", 409 "Two-factor is already on." |
| `POST /api/me/mfa/enable` | signed in, `code` | `200 {"recovery_codes": ["xxxx-xxxx-xxxx", …10]}` | 400 `code` "That code didn't match. Try the newest one.", 409 |
| `POST /api/me/mfa/disable` | signed in, `code` or `recovery_code` | `204` | 400 |
| `POST /api/me/mfa/recovery` | signed in, `code` | `200 {"recovery_codes": […]}` (new set) | 400 |
| `POST /api/auth/mfa` | the `taskwell_mfa` cookie, plus `code` or `recovery_code` | starts the session like a normal sign-in: `200` with the same body as signin | 400 `code` / `recovery_code` messages, 401 "Your sign-in timed out. Start again." |

Changes to the existing sign-ins (`signin`, `otp/verify`, `signup/verify` doesn't apply, `google/callback`):
- When the user has two-factor enabled, `signin` and `otp/verify` don't start a session. They set the `taskwell_mfa` cookie and answer `200 {"mfa_required": true}`.
- The Google callback redirects to `{app}/signin/2fa` instead.
- `/api/auth/mfa` is rate-limited by IP like the other auth endpoints, plus 5 wrong codes per pending sign-in, after which the cookie is cleared (401).
- Turning two-factor on or off sends an email notice ("Two-factor sign-in turned on").
- Turning it off also revokes the user's other sessions, as a password change does.
- API tokens are unaffected.

## Frontend (user app)

- **Settings → Profile** gets a new "Two-factor sign-in" section.
  - While it's off: "Turn on". This asks for the password (or an email code for accounts without one), then shows the secret in a copyable mono box with a "Open in authenticator app" link (the `otpauth_url`; there's no QR library). It then asks for a 6-digit code, and finally shows 10 recovery codes with "Copy" and "Download .txt" and a checkbox "I saved these codes" before "Done".
  - While it's on: a status line, "N recovery codes left", "New recovery codes" (asks for a code) and "Turn off" (asks for a code or a recovery code).
- **Sign-in**: when a response has `mfa_required`, go to `/signin/2fa`, a new route in the `(auth)` group. It has a 6-digit code input (`inputmode="numeric"`, `autocomplete="one-time-code"`), "Use a recovery code instead", and "Verify". On success it goes where a normal sign-in goes.

## Verification

- `go test`: setup, enable and disable; a code can't be replayed; a recovery code works once; each sign-in method answers `mfa_required`; the cookie expiry; the attempt cap.
- Curl through the flow using codes computed in a small Go test helper, or with `oathtool` if it's installed.
- `npm run build`.
- Integration: turn it on, sign out, sign in with a code, then sign in with a recovery code, then turn it off.
