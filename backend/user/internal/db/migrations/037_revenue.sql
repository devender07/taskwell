-- Revenue (docs/revenue-plan.md): Paddle's money events, mirrored from verified webhooks and the -paddle-backfill.
-- Amounts are minor units of currency_code (lowercased); an event older than the row (paddle_updated_at) doesn't
-- overwrite it.
create table paddle_transactions (
    transaction_id text primary key,                      -- txn_…, stored once completed or paid
    user_id uuid references users (id) on delete set null,
    subscription_id text,
    customer_id text,
    status text not null,
    currency_code text not null,
    subtotal bigint not null,
    tax bigint not null,
    total bigint not null,
    earnings bigint,                                      -- after Paddle's fee and tax; null until Paddle sends it
    billed_at timestamptz not null,
    paddle_updated_at timestamptz not null,
    created_at timestamptz not null default now()
);
create index on paddle_transactions (billed_at);

create table paddle_adjustments (
    adjustment_id text primary key,                       -- adj_…
    transaction_id text,
    user_id uuid references users (id) on delete set null,
    action text not null,                                 -- refund, credit, chargeback, chargeback_reverse, …
    status text not null,                                 -- pending_approval, approved, rejected, reversed
    currency_code text not null,
    total bigint not null,
    created_at timestamptz not null,                      -- Paddle's
    paddle_updated_at timestamptz not null
);
create index on paddle_adjustments (created_at);

alter table paddle_subscriptions add column unit_amount bigint;  -- the first item's price unit_price.amount
