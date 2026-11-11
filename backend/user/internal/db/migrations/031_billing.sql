-- Plans per account, paid with Paddle (docs/billing-plan.md); a workspace has its creator's limits.
-- users.plan is the cached plan in force, derived from the mirrored Paddle rows below (free without a subscription
-- granting access) unless plan_comped.
alter table users
    add column plan text not null default 'free' check (plan in ('free', 'starter', 'pro', 'advanced')),
    add column plan_comped boolean not null default false;  -- set by a platform admin; Paddle events leave it alone

-- Paddle state mirrored from verified webhook events; an event older than the row (paddle_updated_at) doesn't overwrite it.
create table paddle_customers (
    customer_id text primary key,                         -- ctm_…
    user_id uuid references users (id) on delete set null,
    email text not null default '',                       -- '' until customer.created/updated arrives
    paddle_updated_at timestamptz,                        -- null on a placeholder a subscription event made
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
    currency_code text,                                   -- lowercased
    next_billed_at timestamptz,
    scheduled_change_action text,                         -- cancel, pause, resume
    scheduled_change_at timestamptz,
    paddle_updated_at timestamptz not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);
create index on paddle_subscriptions (user_id);

delete from system_config where key = 'upload_quota_mb';     -- storage now comes from the plan

insert into system_config (key, value, secret, description) values
  ('paddle_environment', '', false, 'Paddle environment for billing: sandbox or production; anything else turns billing off'),
  ('paddle_api_key', '', true, 'Paddle server API key; empty turns billing off'),
  ('paddle_webhook_secret', '', true, 'Secret key of the Paddle notification destination at /api/billing/webhook')
on conflict (key) do nothing;
