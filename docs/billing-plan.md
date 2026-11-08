# Billing plan

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
