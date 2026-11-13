# Beta mode

Taskwell launches as a free beta before there's a registered company. Staff turn beta mode on from the admin app's
overview page. While it's on, nothing in the product asks for money or points at legal text that needs a company:

- No pricing, checkout or billing anywhere: the pricing page, Billing settings, the trial and past-due banners and
  every upgrade prompt or link are gone. The API refuses billing actions the same way it does while Paddle isn't
  set up.
- Every account gets the Free plan's limits (as today with no subscription). Plan feature flags still apply, but a
  feature locked by the plan is hidden, not shown as an upgrade. Limit messages don't mention upgrading.
- The privacy notice and terms are replaced by one short beta notice (below). Subprocessors stays, without the
  "draft for legal review" line.

Turning it off brings everything back unchanged. Nothing is deleted.

Out of scope: per-account beta flags, a waitlist, a "Beta" badge, changing plan limits (the Config page already does).

## Config

Migration `045_beta_mode.sql`:

```sql
insert into system_config (key, value, secret, description) values
  ('beta_mode', 'false', false, 'Beta: hide pricing, billing and upgrade prompts, and show the beta notice instead of the privacy notice and terms (true or false)')
on conflict (key) do nothing;
```

Admin config validation: `beta_mode` must be `true` or `false` ("Use true or false."). A missing or unreadable row is
`false`.

## API contract (user API)

- `GET /api/beta`, public (no auth, `web.Public`), `200 {"beta": true|false}`. Read through `sysconfig` (cached
  up to 10 s, invalidated by the admin config event). Lives in the billing package.
- Billing while beta: `billing.settings` returns `errBillingOff` when `beta_mode` is `true`, without logging an
  error. So `GET /api/billing` answers `"enabled": false`, and `POST /api/billing/checkout`, `/change`,
  `/change/preview`, `/payment-method`, `/portal` answer `503 {"error": "Billing isn't set up yet."}` (the existing
  `errOff`). `POST /api/billing/webhook` still processes deliveries.
- Limit messages: `plan.Load` also reads `beta_mode` and `plan.Beta()` reports it. While beta, these drop the
  upgrade wording (same status codes as today):
  - workspace create: "Your plan allows 1 workspace."
  - project create, invite over the member limit: "This workspace's plan allows 1 project." / "… 1 member."
  - invite into a full workspace: "This workspace is full."
  - upload over storage: "This workspace is out of storage (2 GB). Delete some files."
  - AI run on a plan without AI: "AI test runs aren't available during the beta."

Admin API: unchanged (`POST /api/config/beta_mode`, FormData `value`, answers the Setting).

## Frontend

User app:
- `getBeta()` in `src/api.ts`: one memoized `GET /api/beta` per page load, `false` when it fails.
- `/pricing`, `/_app/billing`, `/$workspace/settings/billing` and `/$workspace/upgrade`: in `beforeLoad`, beta →
  `/pricing` redirects to `/signin`, the others throw `notFound()`.
- Settings nav hides Billing; trial and past-due banners don't render; `upgradesFor` returns `{}` (so locked
  features are hidden and `requireFeature` 404s); no link to `/pricing` or billing settings renders anywhere
  (locked links, workspace and member limit prompts, welcome page).
- `/privacy` and `/terms` render the beta notice (title "Beta notice", no draft line). `LegalPage` nav shows
  "Beta notice" (`/terms`) and "Subprocessors" while beta.
- Auth card footer: "By continuing, you agree to the beta notice." (links `/terms`). Signup checkbox: "I'm 18 or
  older and accept the beta notice." (links `/terms`); the field and its error stay as they are.
- Landing (`landing.html` + `landing.ts`): the pricing nav link and the `#pricing` section carry `data-paid`;
  `landing.ts` fetches `/api/beta` and hides `[data-paid]` when it's true.

Admin app overview page: a "Beta mode" card above the stats with the state and one button, "Turn on beta mode" /
"Turn off beta mode", posting `/config/beta_mode`. Text: "While it's on, pricing, billing and upgrade prompts are
hidden, everyone gets the Free plan's limits, and the beta notice replaces the privacy notice and terms."

### Beta notice text

> **Taskwell is in beta.** It's free while it's in beta, run by an independent developer, and offered as is: it may
> change, have bugs or be unavailable, and it comes with no warranty. Don't rely on it for anything critical, and
> keep your own copies of anything important. We'll email you before the beta ends or before anything you made is
> deleted.
>
> **What we collect.** Your name, email and password (stored hashed), what you and your team put in Taskwell
> (workspaces, projects, issues, comments, notes, files), and technical data needed to run it: sign-in sessions,
> IP addresses and browser details for security, and error logs.
>
> **What we do with it.** Only run Taskwell: sign you in, show your work to the people you share it with, send the
> emails you asked for, and keep the service secure. We don't sell it or use it for ads. The services that host it
> and send email for us are listed under Subprocessors.
>
> **Your choices.** You can export your data and delete your account under Settings → Your data. For anything else,
> email privacy@taskwell.dev.
>
> **Using Taskwell.** You must be 18 or older. Don't use it for anything illegal, to harm others, or to try to break
> it. We can suspend accounts that do.
