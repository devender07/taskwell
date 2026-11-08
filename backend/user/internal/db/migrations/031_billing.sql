-- Plans per account, paid with Paddle (docs/billing-plan.md); a workspace has its creator's limits.
-- plan is always the plan in force: free without a subscription in force.
alter table users
    add column plan text not null default 'free' check (plan in ('free', 'starter', 'pro', 'advanced')),
    add column plan_comped boolean not null default false,  -- set by a platform admin; Paddle events leave it alone
    add column paddle_customer_id text,
    add column paddle_subscription_id text unique,          -- the subscription in force, null without one
    add column subscription_status text,                    -- Paddle's status, null without a subscription
    add column billing_currency text,                       -- the subscription's ISO currency code, lowercased
    add column billing_interval text check (billing_interval in ('month', 'year')),
    add column renews_at timestamptz,                       -- next_billed_at
    add column cancel_at_period_end boolean not null default false;

delete from system_config where key = 'upload_quota_mb';     -- storage now comes from the plan

insert into system_config (key, value, secret, description) values
  ('paddle_environment', '', false, 'Paddle environment for billing: sandbox or production; anything else turns billing off'),
  ('paddle_api_key', '', true, 'Paddle server API key; empty turns billing off'),
  ('paddle_webhook_secret', '', true, 'Secret key of the Paddle notification destination at /api/billing/webhook')
on conflict (key) do nothing;
