# Billing plan

> **Current contract: the "Paddle catalog and pricing page" section at the end.** It supersedes the plans, prices,
> checkout, plans endpoint and pricing cards described above it; account ownership, limits enforcement, webhook
> signature, sync rules, portal and admin comps stay as described.


Plans belong to an **account** (a user), not to a workspace. The account that creates a workspace owns it
(`workspaces.created_by`), and every workspace it owns gets the limits of its plan. Paid through **Paddle** (merchant
of record: charges, VAT / sales tax / GST, payout to an Indian bank). Customers in India pay in INR (card, UPI
AutoPay), everyone else in USD. No AI credits.

| Plan | Price / month | Workspaces | Projects per workspace | Members per workspace | Storage per workspace |
| --- | --- | --- | --- | --- | --- |
| Starter | Free | 1 | 1 | 5 | 2 GB |
| Pro | $12 · ₹699 | 2 | 2 | 10 | 100 GB |
| Ultimate | $49 · ₹2,499 | Unlimited | Unlimited | Unlimited | Unlimited |

- Workspaces: those the account created (`created_by`). Joining someone else's workspace never counts.
- Projects: every project in the workspace, archived included (not deleted ones).
- Members: members plus pending invites, guests included.
- A workspace whose creator is gone (`created_by` null) gets Starter limits.
- Over a limit (after a downgrade): nothing is removed; creating workspaces / projects, inviting, joining and uploading
  stop until it's under again.
- Every feature on every plan. Monthly only. Prices exclude tax (Paddle adds it at checkout).

## Paddle objects

`deploy/paddle-setup.sh <api key> <taskwell api base url>` (sandbox by default, `PADDLE_API` for live): products
"Taskwell Pro" / "Taskwell Ultimate" (`tax_category=saas`), one monthly price each, base USD 1200 / 4900 with
`unit_price_overrides` `[{country_codes: ["IN"], unit_price: {amount: "69900" / "249900", currency_code: "INR"}}]`,
and the notification destination at `<api>/api/billing/webhook`. Prints price ids and the webhook secret. Reuses what
exists (and updates a reused price's amounts to these).

## Schema: migration `031_billing.sql` (rewritten in place; never deployed)

```sql
alter table users
    add column plan text not null default 'starter' check (plan in ('starter', 'pro', 'ultimate')),
    add column plan_comped boolean not null default false,  -- set by a platform admin; Paddle events leave it alone
    add column paddle_customer_id text,
    add column paddle_subscription_id text unique,
    add column paddle_checkout_id text,                     -- the last transaction our checkout made
    add column subscription_status text,
    add column billing_currency text check (billing_currency in ('usd', 'inr')),
    add column renews_at timestamptz,
    add column cancel_at_period_end boolean not null default false;
delete from system_config where key = 'upload_quota_mb';
-- plus the six paddle_* system_config keys (unchanged)
```

No billing columns on `workspaces`. Local databases that ran an earlier 031 are fixed by hand (integration step).

## `system_config` keys (unchanged)

`paddle_environment`, `paddle_api_key` (secret), `paddle_client_token`, `paddle_webhook_secret` (secret),
`paddle_price_pro`, `paddle_price_ultimate`. Billing is on when key, token and both prices are set.

## Limits: `internal/plan`

`plan.Plans` with `Workspaces`, `Projects`, `Members`, `Storage` (0 = unlimited) and:

- `ForUser(ctx, q, userID)`: the account's plan.
- `ForWorkspace(ctx, q, workspaceID)`: its owner's plan (Starter when no owner).

Checks and messages (`402`):

- Create workspace (`POST /api/workspaces`, and any other path that creates one for an existing user; the first
  workspace at sign-up is always allowed): "Your plan allows 1 workspace. Upgrade to create more." (number and
  plural from the plan).
- Create project (every path: the project create endpoint, CSV / GitHub import if they create projects, MCP goes
  through the API): "This workspace's plan allows 1 project. Its owner can upgrade for more."
- Invite / join / storage: as today, limits from the owner's plan; wording "This workspace's plan allows 5 members.
  Its owner can upgrade for more." / "This workspace is full. Ask its owner to upgrade." / "This workspace is out of
  storage (2 GB). Delete some files or ask its owner to upgrade."

The `billing.manage` permission is removed (only the account itself pays).

## Currency

The account's `billing_currency` once it has paid; else INR when its profile timezone (else the request's `tz`) is
Indian; else USD. The charged currency is Paddle's, from the billing address; the webhook stores it.

## API contract

### `GET /api/billing/plans` (public)

`200` `{"plans": [{"id", "name", "workspaces": n|null, "projects": n|null, "members": n|null, "storage_bytes":
n|null, "prices": {"usd": n|null, "inr": n|null}}]}` (null limit = unlimited; prices as before, from Paddle, cached).

### `GET /api/billing?tz=` (signed in: the caller's account)

`200` `{"plan", "comped", "status": string|null, "currency": "usd"|"inr", "renews_at": string|null,
"cancel_at_period_end": bool, "workspaces": {"used", "limit": n|null}, "enabled": bool}`

### `POST /api/billing/checkout` (FormData `plan` = `pro` | `ultimate`)

Transaction with `custom_data: {user_id}`, `customer_id` when known; stores `paddle_checkout_id` on the user.
`200` `{"transaction_id", "client_token", "environment", "email"}`. `400` "Choose a plan.", `409` "You already have
a subscription. Manage it to change plans." (in force or comped), `503` "Billing isn't set up yet."

### `POST /api/billing/change` (FormData `plan`)

As before for the caller's subscription; `200` the `GET /api/billing` shape. `400`, `409` "You don't have a
subscription yet.", `503`.

### `POST /api/billing/portal`

As before; `200` `{"url"}`. `409` "You don't have a subscription yet.", `503`.

### `GET /api/workspaces/{slug}/billing` (any member; guests `403`, unknown `404`)

The workspace's view of its owner's plan:

`200` `{"plan", "owner": {"id", "name"} | null, "is_owner": bool, "projects": {"used", "limit"}, "members": {"used",
"limit"}, "storage": {"used_bytes", "limit_bytes"}}`

### `POST /api/billing/webhook` (Paddle)

Signature as before. `subscription.*`: the account holding the subscription, else the one in `custom_data.user_id`
whose `paddle_checkout_id` equals the event's `transaction_id`; then sync (same rules as before: active / trialing /
past_due keep the paid plan; paused / canceled → Starter and clear the subscription; comped keeps its plan; an
older subscription's late events are ignored). `adjustment.created`: logged at warn.

Audit: a plan change records `billing.plan_changed` `{from, to}` (actor = the account) in the workspace audit of
every workspace the account owns. Realtime: the account's `/billing` and each owned workspace's `/billing` and
`/storage` paths.

## Admin app

Users page: a Plan column (Starter / Pro / Ultimate, "comped") and "Set plan" (comp / clear comp) moved here from
the Workspaces page: `POST /api/users/{id}/plan` FormData `plan`, `comped`, same rules as before (clearing reads the
Paddle subscription). Workspaces page: the Plan column shows the owner's plan, read-only; its "Set plan" goes away.
Admin audit `user.plan_set`.

## Frontend

- **Settings → Billing** (`/$workspace/settings/billing`), visible to every non-guest member:
  - "This workspace" section: owner's plan line ("On Pro, owned by Ann" / "On your Pro plan"), meters for projects,
    members, storage.
  - "Your plan" section (the caller's own account): plan line ("Pro · renews on 12 Dec" / "Cancels on …" / "Payment
    failed. Update your payment method." / "Comped by Taskwell"), a workspaces meter (`2 of 2 workspaces you own`),
    then the plan cards with Upgrade / Switch plan / Manage billing as before (now account endpoints; no permission
    gate, it's your own account).
- **Plan cards** (billing page and landing page): checks become "1 workspace" / "2 workspaces" / "Unlimited
  workspaces", "1 project per workspace" / "2 projects per workspace" / "Unlimited projects", "Up to 5 members per
  workspace", "2 GB storage per workspace", "Every Taskwell feature". Pricing lead and FAQ say plans are per account
  ("One price for your account, covering every workspace you create"); FAQ "per person or per workspace?" becomes
  "Who pays?" (the person who creates workspaces; members you invite don't need a plan). Pro stays "Most popular".
  Price "/ month".
- Create-workspace dialog and new-project form show the `402` message (check they surface API errors).
- `api.ts`: types and calls for the new endpoints.

## Verification

- `go vet`, `go test`: limits (2nd workspace refused on Starter, allowed on Pro; 2nd project refused on Starter,
  3rd on Pro; owner-less workspace on Starter limits; members and storage from the owner's plan); account checkout /
  change / portal; webhook linking through `paddle_checkout_id` and `user_id`, foreign checkout ignored; audit to
  every owned workspace; admin user plan endpoint.
- Browser against a fake Paddle: billing page as owner and as a non-owner member, workspace and project limits,
  landing cards.
- `npm run build`, `npm run check`.

## Paddle catalog and pricing page (current)

### Catalog (made in the Paddle sandbox; ids are the source of truth)

| Plan | Product | Monthly price | Yearly price | USD month / year |
| --- | --- | --- | --- | --- |
| Starter | `pro_01m6wk3cc4p29t26qnkzbjng3m` | `pri_01m6wk3cmqzf6zhwrsdhq79xhx` | `pri_01m6wk3cxd0b6mdhphsj67m1hk` | 5.00 / 48.00 |
| Pro | `pro_01m6wk3d7377t9adbde1f375ra` | `pri_01m6wk3df6zkt76e4zsz6q1x8a` | `pri_01m6wk3dqt1m882m8a68khw5fk` | 9.99 / 100.00 |
| Advanced | `pro_01m6wk3e2m3w4x2mqjsx5kdgt8` | `pri_01m6wk3eavpqzpc1gjs4ace2p9` | `pri_01m6wk3ekevk50xcfe07vjxbnk` | 49.99 / 500.00 |

7-day trial (card required) on every price; GB / IE / AU overrides; tax mode `location`. Every price and product
carries `custom_data.taskwell_plan` (`starter` / `pro` / `advanced`): **the backend maps a subscription to a plan by
its price's `custom_data.taskwell_plan`**, so prices made later need it too. Default payment link:
`https://taskwell.dev/pricing` (Paddle.js there opens `?_ptxn=` checkouts by itself).

**One trial per account.** Each price has a copy without the trial (quantity fixed at 1, `custom_data.trial` =
`none`), in `tiers.ts` as `noTrial`:

| Plan | Monthly, no trial | Yearly, no trial |
| --- | --- | --- |
| Starter | `pri_01m6wq0rxmnyjvzjex4bf82vcj` | `pri_01m6wq0sdxk937m927rr54d6g3` |
| Pro | `pri_01m6wq0sxs3t3h70px2h59y38b` | `pri_01m6wq0tfs1z76jdtnsp5av839` |
| Advanced | `pri_01m6wq0tzz40vgs3g3sw7c4sct` | `pri_01m6wq0vg2br8ejp563cw79zqc` |

`GET /api/billing` answers `trial_used` (the account has any mirrored subscription); the pricing page then checks out
and switches plans on the no-trial prices. Backstop: a `trialing` subscription linked to an account that had an
earlier one is activated (`POST /subscriptions/{id}/activate`, billed now) from its webhook. A new email is a new
account, so it gets a new trial; the card required at checkout is the guard there.

### Plans and limits (`internal/plan`, easy to edit)

An account with no subscription in force is on **Free**. Ids: `free`, `starter`, `pro`, `advanced`.

| Plan | Workspaces | Projects per workspace | Members per workspace | Storage per workspace |
| --- | --- | --- | --- | --- |
| Free | 1 | 1 | 5 | 2 GB |
| Starter | 1 | 5 | 10 | 20 GB |
| Pro | 3 | Unlimited | 25 | 100 GB |
| Advanced | Unlimited | Unlimited | Unlimited | Unlimited |

Migration `031_billing.sql` (rewritten in place again): `users.plan` default `'free'`, check in
(`free`,`starter`,`pro`,`advanced`); add `billing_interval text check in ('month','year')`; **drop**
`paddle_checkout_id`; `billing_currency` becomes free text (any ISO code, lowercased; no check). `system_config`:
`paddle_environment` (seeded **empty**: billing refuses to run, logging an error, until it's exactly `sandbox` or
`production`), `paddle_api_key` (secret), `paddle_webhook_secret` (secret). The client token and price-id keys are
gone (the frontend has them).

### Checkout: client-side, signed custom_data

The pricing page opens Paddle.js checkout with the exact price shown: `Paddle.Checkout.open({items: [{priceId,
quantity: 1}], customer: {email}, customData, settings: {displayMode: 'overlay', variant: 'one-page', successUrl:
<origin>/welcome}})`. To link the subscription to the account without trusting client data:

- `POST /api/billing/checkout` (signed in; FormData `price_id`): `200` `{"custom_data": {"user_id": "<uuid>",
  "sig": "<hex HMAC-SHA256 of user_id, key derived from jwt_secret like the export tokens>"}, "email": "<caller's>"}`.
  `409` "You already have a subscription. Manage it to change plans." (in force, or comped). `400` "Choose a plan."
  for an empty `price_id`. `503` "Billing isn't set up yet." when `paddle_environment` isn't sandbox/production. No
  Paddle API call here.
- Webhook `subscription.*`: account = the one holding `paddle_subscription_id`; else `custom_data.user_id` **only
  if** `custom_data.sig` verifies (hmac.Equal) **and** that account has no other subscription in force. Plan = the
  subscription's `items[0].price.custom_data.taskwell_plan` (unknown → error, Paddle retries). Interval =
  `items[0].price.billing_cycle.interval`. Status `trialing` keeps the plan (as today). Rest of sync unchanged; a
  subscription no longer in force returns the account to `free`.
- `POST /api/billing/change` (FormData `price_id`): reads the price (`GET /prices/{id}`) to check its
  `taskwell_plan`, then `PATCH /subscriptions/{id}` with it, `proration_billing_mode: prorated_immediately` (an
  interval change works the same way). `400` "Choose a plan." for an unknown price or the current one.
- `GET /api/billing` gains `"interval": "month"|"year"|null`; `currency` is the subscription's (null without one).
  `enabled` = environment valid and API key set. `GET /api/billing/plans` is **removed** (prices come from Paddle.js,
  features from the frontend tiers file).
- Portal unchanged.

### Frontend

- Dependency `@paddle/paddle-js` (asked for). `src/paddle.ts`: `getPaddle()` = `initializePaddle({environment:
  import.meta.env.VITE_PADDLE_ENV, token: import.meta.env.VITE_PADDLE_CLIENT_TOKEN})` once; **throws** when
  `VITE_PADDLE_ENV` isn't `sandbox`/`production` or the token is missing. Env in `frontend/user/.env.development`
  and `.env.production` (sandbox values for now) and a new `frontend/user/.env.example`.
- `src/pricing/tiers.ts`: `export interface Tier { id: 'starter' | 'pro' | 'advanced'; name: 'Starter' | 'Pro' |
  'Advanced'; description: string; features: string[]; priceId: { month: string; year: string }; featured?: boolean
  }` and `TIERS` with the ids above (Pro featured, "Most popular").
- `src/pricing/pricing-table.tsx`: monthly / yearly toggle (in the URL via the owning route's `validateSearch`
  where it has one); prices from `Paddle.PricePreview({items: all six prices})`, shown **only** as Paddle's
  `formattedTotals.total` strings (no math, no Intl, no reformatting), with "/ month" or "/ year"; no
  `address.countryCode` (no server-side country header in this stack, so Paddle detects it from the IP). Each tier:
  name, description, price, "7-day free trial", Subscribe button, features. Buttons:
  - signed out → `/signup?redirect=/pricing?interval=…` (or signin if signup has no redirect; check `safeRedirect`).
  - signed in, no subscription → POST checkout, then `Paddle.Checkout.open(...)` as above with the email prefilled.
  - subscribed → current tier+interval "Current plan"; others "Switch plan" (confirm dialog, POST change).
- Routes: public `/pricing` (pricing table + "Every plan includes" + FAQ moved from the landing page; works signed in
  or out; loads Paddle.js so `?_ptxn=` checkouts open), signed-in `/welcome` ("You're subscribed" / trial text,
  polls `/api/billing` a few times until the plan shows, link to the dashboard). Landing page: the pricing section
  and FAQ become a short "Plans from Starter to Advanced, 7-day free trial" block linking to `/pricing`; nav
  "Pricing" → `/pricing`. Settings → Billing: "This workspace" as now; "Your plan" shows plan, interval, status,
  renewal, Manage billing, and the same `PricingTable`.
- CSP (production headers in `vite.config.ts`): keep Paddle domains; make sure checkout `variant: 'one-page'` and
  PricePreview work (Paddle.js calls `*.paddle.com`; frames `buy.paddle.com` / `sandbox-buy.paddle.com`).
- Admin: plan names Free / Starter / Pro / Advanced in both pages and the Set plan dialog.

## Fulfillment: mirrored customers and subscriptions (current)

Paddle state is mirrored from verified webhook events into two tables; the account's effective plan is derived from
them. Folded into `031_billing.sql` (still never deployed):

```sql
-- users keeps only: plan (cached effective plan, read by limits), plan_comped. The other paddle/subscription columns
-- on users go away (they live on the subscription row).
create table paddle_customers (
    customer_id text primary key,                         -- ctm_…
    user_id uuid references users (id) on delete set null,
    email text not null default '',                       -- '' until customer.created/updated arrives
    paddle_updated_at timestamptz,                        -- the entity's updated_at; older events don't overwrite
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);
create table paddle_subscriptions (
    subscription_id text primary key,                     -- sub_…
    customer_id text not null references paddle_customers (customer_id),
    user_id uuid references users (id) on delete set null,
    status text not null,                                 -- active, trialing, past_due, paused, canceled
    price_id text not null,
    product_id text not null,
    plan text not null,                                   -- price custom_data.taskwell_plan
    billing_interval text,
    currency_code text,
    next_billed_at timestamptz,
    scheduled_change_action text,                         -- cancel, pause, resume
    scheduled_change_at timestamptz,
    paddle_updated_at timestamptz not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);
create index on paddle_subscriptions (user_id);
```

- **Verification first**: the stdlib `Paddle-Signature` check runs on the raw body before anything is parsed; a bad
  signature → `400` (never 2xx). A handler error → `500` so Paddle retries.
- **Routing** (typed structs per entity): `subscription.created|updated|canceled|activated|trialing|past_due|paused|
  resumed` → `subscriptionEvent`; `customer.created|updated` → `customerEvent`; `transaction.completed` →
  `transactionEvent` (ensures the customer row exists, links it to the account when the transaction's
  `custom_data` is signed, logs); `adjustment.created` → warn log; anything else → `200`, ignored.
- **Idempotent, order-safe**: every write is `insert … on conflict (id) do update … where excluded.paddle_updated_at
  >= <table>.paddle_updated_at`. A subscription event for an unknown customer inserts a placeholder customer
  (`email ''`) that customer events fill in. Everything comes from the event payload (no API re-read).
- **Linking to an account**: a subscription row keeps its `user_id` once set. A new one gets it only from signed
  `custom_data` (`user_id` + `sig`, as the checkout endpoint makes), and only when that account has no other
  subscription granting access. The customer row gets the same `user_id`.
- **Access helper** `grantsAccess(status string) bool`: `active`, `trialing`, `past_due` (grace while Paddle retries)
  → true; `paused`, `canceled` and anything else → false. A `scheduled_change` never revokes access by itself.
- **Effective plan**: after each subscription write (same transaction), the account's `users.plan` = the plan of its
  most recently updated subscription that grants access, else `free`; `plan_comped` keeps the admin's plan. Plan
  changes are audited and published as before.
- `GET /api/billing`: `status`, `currency`, `interval`, `renews_at` (`next_billed_at`), `cancel_at_period_end`
  (`scheduled_change_action = 'cancel'`) come from that subscription row (nulls without one).
- **Portal**: signed in first; the customer and subscription come from `paddle_subscriptions` by the session's user
  (never from the client); `POST /customers/{customer_id}/portal-sessions` with `subscription_ids`. `409` without one.
- **Change**: the session user's subscription granting access, as before.
- **Admin clear comp**: the plan comes from the mirrored rows (no Paddle call).
