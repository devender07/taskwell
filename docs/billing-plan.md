# Billing plan

Flat-priced plans per workspace (not per seat), paid through **Paddle** (merchant of record: it charges the customer,
collects and files VAT / sales tax / GST, and pays out to an Indian bank). Stripe and Razorpay are not used. Customers
in India pay in INR (cards, UPI AutoPay), everyone else in USD. No AI credits: AI testing keeps its own monthly limit.

| Plan | Price / month | Members | Storage |
| --- | --- | --- | --- |
| Starter | Free | 5 | 2 GB |
| Pro | $29 · ₹1,499 | 10 | 100 GB |
| Ultimate | $79 · ₹3,999 | Unlimited | Unlimited |

Members means members plus pending invites, guests included. Every feature is on every plan; plans differ only in
members and storage. Monthly billing only. Prices exclude tax (Paddle adds it at checkout by the buyer's country).

Out of scope: yearly prices, trials, per-seat pricing, invoices in the app (Paddle's customer portal has them).

## Paddle objects

Made once per environment (sandbox, then live) by `deploy/paddle-setup.sh <api key> <api base url of Taskwell>`:

- Products "Taskwell Pro" and "Taskwell Ultimate" (`tax_category=saas`).
- One monthly price each (`billing_cycle={interval: month, frequency: 1}`): base `unit_price` USD 2900 / 7900, and
  `unit_price_overrides` `[{country_codes: ["IN"], unit_price: {amount: "149900" / "399900", currency_code: "INR"}}]`.
  Paddle picks the override from the buyer's country at checkout, so the currency follows their billing address.
- A notification destination (webhook) at `<api>/api/billing/webhook` for the events below; the script prints its
  secret and the two price ids.

The script reuses products/prices it finds (`custom_data.taskwell_plan`) and skips what exists. Sandbox uses
`https://sandbox-api.paddle.com`, live `https://api.paddle.com`; the script takes `PADDLE_API` to switch.

## Schema: migration `031_billing.sql` (rewritten in place; never deployed)

```sql
alter table workspaces
    add column plan text not null default 'starter' check (plan in ('starter', 'pro', 'ultimate')),
    add column plan_comped boolean not null default false,
    add column paddle_customer_id text,
    add column paddle_subscription_id text unique,
    add column paddle_checkout_id text,                     -- the last transaction our checkout made
    add column subscription_status text,                    -- Paddle's status, null without a subscription
    add column billing_currency text check (billing_currency in ('usd', 'inr')),
    add column renews_at timestamptz,                       -- next_billed_at
    add column cancel_at_period_end boolean not null default false;
delete from system_config where key = 'upload_quota_mb';
```

## `system_config` keys (admin app group "Payments": prefix `paddle_`)

- `paddle_environment`: `sandbox` or `production` (default `sandbox`).
- `paddle_api_key` (secret): server API key.
- `paddle_client_token`: client-side token for Paddle.js (public by design).
- `paddle_webhook_secret` (secret): the notification destination's secret.
- `paddle_price_pro`, `paddle_price_ultimate`: price ids (`pri_…`).

Billing is on when `paddle_api_key`, `paddle_client_token` and both price ids are set. Off: pages show "Billing isn't
set up yet." and limits still apply.

## Limits (unchanged)

`internal/plan`: invites and joins `402`, storage via `file.room` / `storage` (`quota_bytes: null` = unlimited).

## Currency

- A workspace with a subscription shows its `billing_currency`.
- Otherwise display currency is INR when the profile timezone (else the browser's, `tz` param) is Indian, else USD.
- The charged currency is Paddle's decision from the billing address; the webhook stores what was charged.

## API contract

Permission `billing.manage` ("Manage billing"), group Workspace.

### `GET /api/billing/plans` (public)

`200` `{"plans": [{"id", "name", "members": n|null, "storage_bytes": n|null, "prices": {"usd": n|null, "inr":
n|null}}]}`. Smallest units. Starter `{"usd": 0, "inr": 0}`. Paid amounts read from Paddle (`GET /prices/{id}`: base
`unit_price` for USD, the `IN` override for INR), cached an hour (a failure a minute); null when off or unreachable.

### `GET /api/workspaces/{slug}/billing?tz=`

`200` `{"plan", "comped", "status": string|null, "currency": "usd"|"inr", "renews_at": string|null,
"cancel_at_period_end": bool, "members": {"used", "limit"}, "storage": {"used_bytes", "limit_bytes"},
"can_manage": bool, "enabled": bool}`. Guests `403`. `404` unknown workspace.

### `POST /api/workspaces/{slug}/billing/checkout` (FormData `plan` = `pro` | `ultimate`)

Creates a Paddle transaction (`POST /transactions`: `items: [{price_id, quantity: 1}]`, `custom_data:
{workspace_id}`, `customer_id` when the workspace has one) and answers what Paddle.js needs to open it:

- `200` `{"transaction_id": "txn_…", "client_token": "…", "environment": "sandbox"|"production", "email": "<caller's
  email>"}`
- `400` "Choose a plan.", `403` "You don't have permission to manage billing.", `409` "This workspace already has a
  subscription. Manage it to change plans." (subscription in force or comped), `503` "Billing isn't set up yet."

### `POST /api/workspaces/{slug}/billing/change` (FormData `plan`)

`PATCH /subscriptions/{id}` with `items: [{price_id, quantity: 1}]`, `proration_billing_mode:
prorated_immediately`, then sync. `200` the billing state (same shape as GET). `400` "Choose a plan." (incl. the
current plan), `409` "This workspace has no subscription yet.", `403`, `503`.

### `POST /api/workspaces/{slug}/billing/portal`

`POST /customers/{customer_id}/portal-sessions` with `subscription_ids: [id]`; `200` `{"url":
urls.general.overview}` (cancel, payment method, invoices live there). `409` "This workspace has no subscription
yet.", `403`, `503`.

### `POST /api/billing/webhook` (public, Paddle)

`Paddle-Signature: ts=<unix>;h1=<hex>`: HMAC-SHA256 of `<ts>:<raw body>` with `paddle_webhook_secret`, any `h1`
matches (constant time), `ts` within 5 minutes; bad → `400` "Invalid signature.". Body capped at 1 MB. Unknown events
→ `200`.

- `subscription.created`, `subscription.updated`, `subscription.activated`, `subscription.past_due`,
  `subscription.paused`, `subscription.resumed`, `subscription.canceled`: workspace from `data.custom_data.workspace_id`
  only together with the event's `transaction_id` equal to the workspace's `paddle_checkout_id` (custom_data alone
  isn't proof: anyone can open a Paddle checkout with any custom_data), or else by `paddle_subscription_id`; then
  sync. Checkout stores the transaction id in `paddle_checkout_id`.
- `adjustment.created` (refunds and chargebacks): audit `billing.refunded` / `billing.disputed` (`action` refund vs
  chargeback) via the subscription id, logged at warn.

Sync = `GET /subscriptions/{id}` (current object wins over event order): plan from `items[0].price.id`; status;
`currency_code` lowercased; `renews_at` = `next_billed_at`; `cancel_at_period_end` = `scheduled_change.action ==
"cancel"`; `paddle_customer_id` = `customer_id`. `active`, `trialing`, `past_due` keep the paid plan; `paused`,
`canceled` set Starter and clear `paddle_subscription_id`. A comped workspace keeps its plan. Plan changes record
`billing.plan_changed` `{from, to}` (actor null). An older subscription's events after the workspace moved to a
newer one are ignored.

Paddle calls: `net/http`, JSON bodies, `Authorization: Bearer <api key>`, base by `paddle_environment`, 10-second
timeout, base a field for tests; no SDK.

## Admin app

Workspaces page: Plan column and "Set plan" (comp / clear comp). `POST /api/workspaces/{id}/plan` FormData `plan`,
`comped`; clearing a comp reads the Paddle subscription (if any, in force) for its plan, else Starter. `502` when
Paddle can't be read. Admin audit `workspace.plan_set`. Config page group "Payments" for `paddle_` keys.

## Frontend

### Paddle.js

Loaded on demand (only when a checkout opens) from `https://cdn.paddle.com/paddle/v2/paddle.js`, initialised with
the checkout reply's `client_token` and `environment` (`Paddle.Environment.set('sandbox')` for sandbox), then
`Paddle.Checkout.open({transactionId, customer: {email}, settings: {displayMode: 'overlay', successUrl:
<billing page>?checkout=done}})`. The CSP (`user/vite.config.ts` production headers) allows Paddle's checkout
domains (check Paddle's CSP doc for the current list).

### Pricing cards (billing page and landing page share one card design)

- Three cards, same size, stacked on phones. Pro is highlighted: "Most popular" badge and an accent border.
- Each card: name, one-line value statement ("For small teams trying Taskwell" / "For growing teams" / "For larger
  teams and lots of files"), price ("Free", or "$29" + "/ month per workspace"), the CTA, then a check list:
  members, storage, "Every Taskwell feature".
- Below the cards: "Every plan includes" list (projects, issues, list and board views, cycles and roadmap, notes,
  chat, AI testing, MCP and API, webhooks, GitHub and GitLab) and a tax line "Prices exclude tax, added at checkout.
  Customers in India pay in INR, by card or UPI."
- Landing page adds a short FAQ: per person or per workspace; what happens over a limit; cancel any time; payment
  methods; tax invoices.

### Billing page

- Plan line: "Pro · renews on 12 Dec" / "Cancels on 12 Dec" / "Payment failed. Update your payment method." (past
  due) / "Comped by Taskwell".
- Usage meters as now.
- Buttons with `can_manage && enabled`, never when comped: on Starter, paid cards "Upgrade" (checkout → Paddle
  overlay). Subscribed: other paid cards "Switch plan" (confirm "Switch to Ultimate now? You'll be charged the
  prorated difference today."), Starter card nothing, and "Manage billing" (portal) under the cards for cancel,
  payment method and invoices.
- `?checkout=done`: "Thanks. Your plan updates in a few seconds." and refetch; the realtime event also refreshes.

## Verification

- `go vet`, `go test`: Paddle signature (good, bad, stale, two h1); sync from a fake Paddle (active → plan, INR,
  renews_at; scheduled cancel; canceled → Starter; comped untouched; stale subscription ignored); checkout transaction
  body (price, custom_data, customer_id when known) and reply; change (PATCH body, 400 same plan, 409); portal;
  plans from fake prices (base USD, IN override INR); limits as before.
- Paddle sandbox when keys exist: setup script, a test-card checkout, switch plan, cancel in the portal.
- Frontend: `npm run build`, `npm run check`, phone and desktop screenshots of both pricing layouts.
