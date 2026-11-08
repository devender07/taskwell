-- Flat-priced plans per workspace, paid with Paddle (docs/billing-plan.md). plan is always the plan in force.
alter table workspaces
    add column plan text not null default 'starter' check (plan in ('starter', 'pro', 'ultimate')),
    add column plan_comped boolean not null default false,  -- set by a platform admin; Paddle events leave it alone
    add column paddle_customer_id text,
    add column paddle_subscription_id text unique,
    add column paddle_checkout_id text,                     -- the last transaction our checkout made; links its subscription
    add column subscription_status text,                    -- Paddle's status, null without a subscription
    add column billing_currency text check (billing_currency in ('usd', 'inr')),
    add column renews_at timestamptz,                       -- next_billed_at
    add column cancel_at_period_end boolean not null default false;

delete from system_config where key = 'upload_quota_mb';     -- storage now comes from the plan

insert into system_config (key, value, secret, description) values
  ('paddle_environment', 'sandbox', false, 'Paddle environment for billing: sandbox or production'),
  ('paddle_api_key', '', true, 'Paddle server API key; empty turns billing off'),
  ('paddle_client_token', '', false, 'Paddle.js client-side token (public by design); empty turns billing off'),
  ('paddle_webhook_secret', '', true, 'Secret key of the Paddle notification destination at /api/billing/webhook'),
  ('paddle_price_pro', '', false, 'Paddle price id (pri_...) of the Pro plan'),
  ('paddle_price_ultimate', '', false, 'Paddle price id (pri_...) of the Ultimate plan')
on conflict (key) do nothing;
