# Billing plan

Flat-priced plans per workspace (not per seat), paid with Stripe. Indian accounts see and pay prices in INR, everyone
else in USD. No AI credits: AI testing keeps its own monthly limit.

| Plan | Price / month | Members | Storage |
| --- | --- | --- | --- |
| Starter | Free | 5 | 2 GB |
| Pro | $29 · ₹1,499 | 10 | 100 GB |
| Ultimate | $79 · ₹3,999 | Unlimited | Unlimited |

Members means members plus pending invites, guests included. Monthly billing only.

Out of scope: yearly prices, trials, per-seat pricing, taxes (no `automatic_tax`; prices are shown as they're
charged), invoices in the app (the Stripe customer portal lists them), dunning emails of our own (Stripe sends them).

## Stripe objects

Made once per Stripe account (sandbox and live) by `deploy/stripe-setup.sh <secret key>` with curl:

- Product "Taskwell Pro", price lookup key `pro_monthly`: `unit_amount=2900`, `currency=usd`, recurring monthly,
  `currency_options[inr][unit_amount]=149900`.
- Product "Taskwell Ultimate", price lookup key `ultimate_monthly`: `7900` USD, `399900` INR.
- A customer portal configuration (default): cancel at period end, update payment method, invoice history, switch
  between the two prices (`features[subscription_update]` with both products, `proration_behavior=create_prorations`).
- A webhook endpoint at `<api>/api/billing/webhook` with the events below; the script prints its signing secret.

The script is idempotent enough: it reuses prices found by lookup key and skips what exists.

Prices aren't stored in Taskwell. The API reads them from Stripe by lookup key (`expand[]=data.currency_options`)
and keeps them in memory for an hour.

## Schema: migration `031_billing.sql`

```sql
alter table workspaces
    add column plan text not null default 'starter' check (plan in ('starter', 'pro', 'ultimate')),
    add column plan_comped boolean not null default false,  -- set by a platform admin; Stripe events leave it alone
    add column stripe_customer_id text unique,
    add column stripe_subscription_id text,
    add column subscription_status text,                    -- Stripe's status, null without a subscription
    add column billing_currency text check (billing_currency in ('usd', 'inr')),
    add column renews_at timestamptz,                       -- current period end
    add column cancel_at_period_end boolean not null default false;
delete from system_config where key = 'upload_quota_mb';     -- storage now comes from the plan
```

`plan` is always the plan in force. Existing workspaces start on Starter; the admin app comps a plan where needed.

## `system_config` keys

- `stripe_secret_key` (secret): a restricted key (`rk_…`) with write on Customers, Checkout Sessions, Customer portal,
  Subscriptions, and read on Prices and Products. Empty: billing is off (pages show "Billing isn't set up yet.") and
  limits still apply.
- `stripe_webhook_secret` (secret): the endpoint's `whsec_…`.

Both seeded empty by the migration, editable in the admin app's config page.

## Limits

One Go file `internal/billing/plans.go`: the three plans with member and storage limits (`0` = unlimited), plus
`Limits(ctx, db, workspaceID)`.

- **Invites** (`createInvite`): members + pending invites ≥ the limit → `402` `{"error": "Your plan allows 5
  members. Upgrade to invite more."}` (wording takes the number).
- **Joining** (`accept`, `acceptByID`): members ≥ the limit → `402` `{"error": "This workspace is full. Ask an
  admin to upgrade its plan."}`.
- **Storage**: `file.room` and the `storage` endpoint take the plan's limit in place of `upload_quota_mb`;
  unlimited → no quota check, `quota_bytes: null`. Full message: "This workspace is out of storage (2 GB). Delete
  some files or upgrade the plan."

A downgrade or cancel removes nobody and nothing; it only stops new invites, joins and uploads while over.

## Currency

- A workspace with `billing_currency` keeps it (a Stripe customer can't switch).
- Otherwise INR when the signed-in user's profile timezone is `Asia/Kolkata` or `Asia/Calcutta`, else USD.
- The landing page guesses the same from the browser's timezone.

## API contract

All under the user API. Permission `billing.manage` ("Manage billing"), group Workspace, admin has it.

### `GET /api/billing/plans` (public)

`200` `{"plans": [{"id": "starter"|"pro"|"ultimate", "name", "members": 5|10|null, "storage_bytes": n|null,
"prices": {"usd": 2900, "inr": 149900} | null}]}`. Amounts are in the smallest unit. `prices` is `{"usd": 0, "inr":
0}` for Starter and `null` for paid plans while billing is off or Stripe can't be reached.

### `GET /api/workspaces/{slug}/billing`

Any member can read it (the sidebar/settings show the plan); `can_manage` says whether they can act.

`200` `{"plan", "comped": bool, "status": string|null, "currency": "usd"|"inr", "renews_at": string|null,
"cancel_at_period_end": bool, "members": {"used", "limit": n|null}, "storage": {"used_bytes", "limit_bytes":
n|null}, "can_manage": bool, "enabled": bool}`

`404` `{"error": "Workspace not found."}`

### `POST /api/workspaces/{slug}/billing/checkout` (FormData `plan` = `pro` | `ultimate`)

Creates the Stripe customer the first time (name: workspace name, email: the caller's, metadata `workspace_id`), then a
Checkout Session: `mode=subscription`, the plan's price, `currency`, `client_reference_id` = workspace id,
`allow_promotion_codes=true`, `integration_identifier=taskwell_billing_<8 letters>`, success
`<app_base_url>/<slug>/settings/billing?checkout=done`, cancel `<app_base_url>/<slug>/settings/billing`. No
`payment_method_types`.

- `200` `{"url": "<checkout url>"}`
- `400` `{"error": "Choose a plan."}` for anything else
- `403` `{"error": "You don't have permission to manage billing."}`
- `409` `{"error": "This workspace already has a subscription. Manage it to change plans."}` (active subscription, or
  comped)
- `503` `{"error": "Billing isn't set up yet."}`

### `POST /api/workspaces/{slug}/billing/portal`

`200` `{"url": "<portal url>"}` back to the billing page. `403` as above, `409` `{"error": "This workspace has no
subscription yet."}` when there's no customer, `503` as above.

### `POST /api/billing/webhook` (public, Stripe)

Verifies `Stripe-Signature` (HMAC-SHA256 of `<t>.<body>` with the webhook secret, 5-minute tolerance, any `v1`
matches) with the stdlib; bad → `400`. Body capped at 1 MB. Unknown events → `200`.

- `checkout.session.completed`: workspace by `client_reference_id`; saves `stripe_customer_id` if not set and
  `stripe_subscription_id`, then syncs the subscription.
- `customer.subscription.created` / `updated` / `deleted`: workspace by the subscription's customer id; sync.
- `invoice.paid`, `invoice.payment_failed`: sync the invoice's subscription.
- `charge.dispute.created`, `charge.refunded`: logged at warn and recorded in the workspace audit log
  (`billing.disputed`, `billing.refunded`) via the charge's customer.

Sync = `GET /v1/subscriptions/{id}` (events can arrive out of order, so the current object wins) then: plan from the
first item's price lookup key; status, currency, `renews_at` from the item's `current_period_end`,
`cancel_at_period_end`. Status `active`, `trialing` or `past_due` keeps the paid plan; anything else (`canceled`,
`unpaid`, `incomplete_expired`, …) sets `plan='starter'` and clears `stripe_subscription_id`. A comped workspace keeps
its plan. Every plan change records `billing.plan_changed` `{from, to}` in the audit log (actor null).

Stripe calls: `net/http` with form encoding, `Authorization: Bearer`, `Stripe-Version: 2026-09-30.endive`, a
10-second timeout; no Stripe SDK. The API base is a field so tests point it at `httptest`.

## Admin app

Workspaces page: a Plan column (Starter / Pro / Ultimate, "comped" when set) and an action to set a workspace's plan
by hand (comp) or clear the comp. Admin API:
`POST /api/workspaces/{id}/plan` FormData `plan`, `comped` (`true`/`false`); clearing a comp returns it to Starter
unless a Stripe subscription is active (then its plan). Recorded in `admin_audit` as `workspace.plan_set`.

## Frontend (user app)

- **Settings → Billing** (`/$workspace/settings/billing`), nav item shown to every member (read-only without
  `billing.manage`). Title "Billing", subtitle "Plans are priced per workspace, not per person."
  - Current plan line: "Pro · renews on 12 Dec" / "Cancels on 12 Dec" / "Payment failed. Update your card." (past due)
    / "Comped by Taskwell".
  - Usage meters: Members `4 of 5`, Storage `1.2 GB of 2 GB` ("Unlimited" for null).
  - Three plan cards (name, price in the workspace's currency with `Intl.NumberFormat`, "/ month", members, storage).
    Current plan says "Current plan". Others: "Upgrade" (POST checkout → `location.assign(url)`) while on Starter;
    "Manage billing" (POST portal) when subscribed; nothing for managers' readers.
  - `?checkout=done` shows "Thanks. Your plan updates in a few seconds." and refetches the billing state a few times.
  - "Manage billing" button under the cards when subscribed.
- Invite form and uploads show the 402 messages as they come.
- Landing page: a Pricing section (the three cards, currency from the browser's timezone, "Get started" to sign up),
  prices from `GET /api/billing/plans`; while those are null, paid cards show no price.
- `src/api.ts`: types and calls.

## Verification

- `go vet`, `go test`: webhook signature (good, bad, stale); sync from a fake Stripe server (active → plan set,
  canceled → Starter, comped untouched); checkout builds the right form (currency, price, client_reference_id) against
  a fake; invite and join refused over the limit; storage quota per plan, unlimited skips it.
- Against a Stripe sandbox when keys are present: `deploy/stripe-setup.sh`, `stripe listen --forward-to`, a test
  card checkout, portal switch and cancel.
- Frontend: `npm run build`.

## Razorpay for INR (follow-up)

Stripe doesn't onboard Indian businesses, so INR goes through Razorpay; USD stays on Stripe until a worldwide gateway
is picked. Routing is by currency: `inr` → Razorpay, `usd` → Stripe. A currency whose gateway has no keys is off
(`enabled: false` for that viewer). The currency rule (profile timezone, else browser) is unchanged.

### Razorpay objects

`deploy/razorpay-setup.sh <key id> <key secret>` creates two monthly plans (`period=monthly`, `interval=1`,
`item[name]=Taskwell Pro` / `Taskwell Ultimate`, `item[amount]=149900` / `399900`, `item[currency]=INR`) and prints
their ids. The webhook is added by hand in the Razorpay Dashboard (`<api>/api/billing/razorpay/webhook`, events
`subscription.activated`, `subscription.charged`, `subscription.pending`, `subscription.halted`,
`subscription.cancelled`, `subscription.completed`, `subscription.updated`); README says how.

### Schema: migration `032_razorpay.sql`

```sql
alter table workspaces
    add column billing_provider text check (billing_provider in ('stripe', 'razorpay')),  -- set with the subscription
    add column razorpay_subscription_id text unique;
```

`system_config` (group Payments in the admin app, since they start with `razorpay_`): `razorpay_key_id`,
`razorpay_key_secret` (secret), `razorpay_webhook_secret` (secret), `razorpay_plan_pro`, `razorpay_plan_ultimate`.

### API changes

- `GET /api/billing/plans`: `prices.inr` comes from the Razorpay plan's `item.amount` (cached an hour like Stripe's);
  `prices.usd` from Stripe. For paid plans `prices` is `{"usd": n|null, "inr": n|null}`, null where that gateway
  is off or unreachable.
- `GET .../billing` gains `"provider": "stripe" | "razorpay" | null` (the subscription's gateway). `enabled` is
  whether the gateway for this viewer's currency (or the subscription's) is configured.
- `POST .../billing/checkout`: for INR, creates a Razorpay subscription (`plan_id`, `total_count=120`,
  `customer_notify=1`, `notes[workspace_id]`), stores `billing_provider='razorpay'`, `razorpay_subscription_id`,
  `billing_currency='inr'`, `subscription_status` from the reply, and answers `{"url": short_url}` (Razorpay's hosted
  payment page; the customer comes back to the billing page by themselves, which updates live). `409` as before when a
  subscription in force exists; a `created` subscription nobody paid is replaced (cancelled first).
- `POST .../billing/change` (new, FormData `plan`): Razorpay only. `PATCH /v1/subscriptions/{id}` with the new
  `plan_id`, `schedule_change_at=now`; then sync. `400` "Choose a plan.", `409` "This workspace has no subscription
  yet." For Stripe subscriptions answer `409` "Manage this subscription in the billing portal."
- `POST .../billing/cancel` (new): Razorpay only. `POST /v1/subscriptions/{id}/cancel` with `cancel_at_cycle_end=1`,
  sync (sets `cancel_at_period_end`). Same 409s.
- `POST .../billing/portal`: Stripe only; for Razorpay `409` "Use switch plan or cancel for this subscription."
- `POST /api/billing/razorpay/webhook` (public): `X-Razorpay-Signature` = hex HMAC-SHA256 of the raw body with
  `razorpay_webhook_secret`, constant-time compare; bad → `400`. Every `subscription.*` event syncs the subscription
  in `payload.subscription.entity.id` (re-read with `GET /v1/subscriptions/{id}`), workspace found by
  `razorpay_subscription_id`.

Razorpay sync: plan from `plan_id` (matching `razorpay_plan_pro` / `_ultimate`); status `active`, `authenticated`
→ the paid plan, `pending` (a charge is retrying) → paid plan, shown like past due; `halted`, `cancelled`,
`completed`, `expired` → Starter and clear `razorpay_subscription_id` and `billing_provider`; `created` → nothing
changes yet. `renews_at` from `current_end` (unix seconds). `cancel_at_period_end` is true once our cancel call
succeeded (Razorpay keeps the subscription `active` until the cycle ends), until the subscription ends.
Statuses are stored as Razorpay sends them; the frontend treats `pending` like `past_due`. Same audit entries.

Razorpay calls: Basic auth key id:secret, `https://api.razorpay.com`, JSON or form bodies, 10-second timeout, base
URL a field for tests. Stdlib only.

### Frontend

- Billing page with `provider === 'razorpay'`: paid cards say "Switch plan" (POST change, confirm dialog "Switch to
  Ultimate now? The new price applies from today."), and under the cards "Cancel subscription" (confirm "Cancel at the
  end of this billing period?") instead of "Manage billing". After cancel the plan line reads "Cancels on …".
- `pending` status reads "Payment failed. Razorpay will retry." like `past_due`.
- Landing page unchanged (prices come from the same endpoint).
