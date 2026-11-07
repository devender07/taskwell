-- Flat-priced plans per workspace, paid with Stripe (docs/billing-plan.md). plan is always the plan in force.
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

insert into system_config (key, value, secret, description) values
  ('stripe_secret_key', '', true, 'Stripe restricted key (rk_...) for billing; empty turns billing off'),
  ('stripe_webhook_secret', '', true, 'Signing secret (whsec_...) of the Stripe webhook endpoint at /api/billing/webhook')
on conflict (key) do nothing;
