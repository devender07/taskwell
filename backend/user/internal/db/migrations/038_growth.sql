-- Growth (docs/growth-plan.md): billing reminder markers, so each email goes out once, and the onboarding checklist's
-- dismissal. Workspaces that already have an issue start dismissed.
alter table paddle_subscriptions add column trial_reminded_at timestamptz, add column past_due_emailed_at timestamptz;
alter table users add column plan_comp_reminded_at timestamptz;
alter table workspaces add column onboarding_dismissed_at timestamptz;
update workspaces w set onboarding_dismissed_at = now() where exists (select 1 from issues_all i where i.workspace_id = w.id);
