# Revenue on the admin overview

The admin Overview counts subscriptions but shows no money: prices and payments live in Paddle. Paddle already sends
us signed webhooks (`/api/billing/webhook`), so we keep a copy of the money events and read revenue from our database,
like everything else on the Overview. A one-time backfill pulls what happened before.

Amounts are kept in minor units (cents, paise) with their currency; we never convert currencies. Customers pay in USD
or INR (docs/billing-plan.md), so every money figure is shown per currency.

## Schema

`backend/user/internal/db/migrations/037_revenue.sql`:

- `paddle_transactions`: `transaction_id text primary key` (txn_…), `user_id uuid references users on delete set
  null`, `subscription_id text`, `customer_id text`, `status text`, `currency_code text` (lowercased, like
  paddle_subscriptions), `subtotal bigint`, `tax bigint`, `total bigint` (minor units: `details.totals`), `earnings
  bigint` (after Paddle's fee and tax, `details.totals.earnings`, null when Paddle doesn't send it), `billed_at
  timestamptz`, `created_at timestamptz default now()`. Index on `billed_at`.
- `paddle_adjustments`: `adjustment_id text primary key` (adj_…), `transaction_id text`, `user_id uuid references users
  on delete set null`, `action text` (refund, credit, chargeback, chargeback_reverse, chargeback_warning…), `status
  text`, `currency_code text`, `total bigint` (minor units, `totals.total`), `created_at timestamptz` (Paddle's).
  Index on `created_at`.
- `paddle_subscriptions` gains `unit_amount bigint` (minor units of the subscribed price's `unit_price.amount`) and
  keeps `currency_code` and `billing_interval`.

## Webhook (user API, `billing` package)

- `transaction.completed` (already handled to link the customer): also upsert the row into `paddle_transactions`
  (user from the customer link or the transaction's custom_data, as today).
- `transaction.updated` / `transaction.paid`: upsert too when the status is `completed` or `paid` (idempotent on id).
- `adjustment.created` / `adjustment.updated` (today only logged): upsert into `paddle_adjustments`.
- `subscription.*` (already mirrored): also store `unit_amount` from the first item's price.
- All upserts keep the newest by Paddle's timestamp, like the subscription mirror.

## Backfill

`go run ./user/cmd/user -paddle-backfill` (wired like the admin CLI flags): with the configured Paddle key and
environment, page through `GET /transactions?status=completed,paid` and `GET /adjustments`, and the subscriptions'
prices, upserting the same rows. Prints counts. Safe to run again. `make paddle-backfill` in backend/Makefile.

## Admin API

`GET /api/stats` gains:

```json
"revenue": {
  "currencies": [
    { "currency": "usd",
      "mrr": 0,              // granting-access subscriptions: unit_amount, yearly divided by 12 (minor units)
      "revenue_30d": 0,      // sum of completed transactions' total billed in the last 30 days
      "earnings_30d": 0,     // same, earnings (after fees and tax); 0 when unknown
      "refunds_30d": 0,      // sum of approved refund adjustments' total in the last 30 days
      "chargebacks_30d": 0,  // same for chargebacks
      "revenue_all": 0,      // all completed transactions
      "per_day": [Day]       // revenue per UTC day, last 30 days, minor units in count
    }
  ],
  "recent": [                // the 10 newest transactions
    { "transaction_id": "…", "email": "…", "name": "…", "currency": "usd", "total": 0, "status": "completed",
      "billed_at": "…" }
  ]
}
```

Currencies with no MRR and no transactions are left out; sorted by revenue_30d, largest first.

## Frontend (admin Overview)

A **Revenue** section right after Billing:

- One row of tiles per currency: MRR, revenue 30 days (with "earnings $x" sub-line when known), refunds 30 days
  and chargebacks 30 days (pink when above 0), all-time revenue. Amounts formatted with `Intl.NumberFormat` in the
  currency (`$1,234.56`, `₹69,900`).
- A 30-day revenue chart per currency (the existing BarChart, its total formatted as money).
- **Recent payments** list card: name, amount, status, date; rows link to the user in Users.
- Empty state when there are no currencies: "No payments yet. Revenue shows here once Paddle sends payments."

## Not doing

- Currency conversion to one total. `ponytail: convert with a daily rate if staff want one number.`
- Invoices, payouts and tax reports: those stay in Paddle.
