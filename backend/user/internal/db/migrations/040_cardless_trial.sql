-- Trials without a payment method (docs/billing-plan.md): such a subscription has no next_billed_at until a payment
-- method is added, so when its trial ends is mirrored on its own.
alter table paddle_subscriptions add column trial_ends_at timestamptz;
