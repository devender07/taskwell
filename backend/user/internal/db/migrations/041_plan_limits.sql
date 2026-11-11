-- AI test runs cost money and Free has no AI budget (plan.AIUSD), so Free no longer has the feature unless an admin
-- already chose its plans.
update system_config set value = 'starter,pro,advanced', updated_at = now()
 where key = 'feature_ai_testing_plans' and value = 'free,starter,pro,advanced';

-- Plan limits (package plan), so the admin app's Config page can change them; the values are the defaults in code.
insert into system_config (key, value, secret, description) values
  ('plan_free_workspaces', '1', false, 'Free plan: workspaces an account can create (0 is unlimited)'),
  ('plan_free_projects', '1', false, 'Free plan: projects per workspace, archived ones included (0 is unlimited)'),
  ('plan_free_members', '1', false, 'Free plan: members per workspace, pending invites included (0 is unlimited)'),
  ('plan_free_storage_gb', '2', false, 'Free plan: storage per workspace in GB (0 is unlimited)'),
  ('plan_free_ai_usd', '0', false, 'Free plan: AI test spend a month in USD, across all the account''s workspaces (0 is none)'),
  ('plan_starter_workspaces', '1', false, 'Starter plan: workspaces an account can create (0 is unlimited)'),
  ('plan_starter_projects', '5', false, 'Starter plan: projects per workspace, archived ones included (0 is unlimited)'),
  ('plan_starter_members', '10', false, 'Starter plan: members per workspace, pending invites included (0 is unlimited)'),
  ('plan_starter_storage_gb', '20', false, 'Starter plan: storage per workspace in GB (0 is unlimited)'),
  ('plan_starter_ai_usd', '1', false, 'Starter plan: AI test spend a month in USD, across all the account''s workspaces (0 is none)'),
  ('plan_pro_workspaces', '3', false, 'Pro plan: workspaces an account can create (0 is unlimited)'),
  ('plan_pro_projects', '0', false, 'Pro plan: projects per workspace, archived ones included (0 is unlimited)'),
  ('plan_pro_members', '25', false, 'Pro plan: members per workspace, pending invites included (0 is unlimited)'),
  ('plan_pro_storage_gb', '100', false, 'Pro plan: storage per workspace in GB (0 is unlimited)'),
  ('plan_pro_ai_usd', '3', false, 'Pro plan: AI test spend a month in USD, across all the account''s workspaces (0 is none)'),
  ('plan_advanced_workspaces', '0', false, 'Advanced plan: workspaces an account can create (0 is unlimited)'),
  ('plan_advanced_projects', '0', false, 'Advanced plan: projects per workspace, archived ones included (0 is unlimited)'),
  ('plan_advanced_members', '0', false, 'Advanced plan: members per workspace, pending invites included (0 is unlimited)'),
  ('plan_advanced_storage_gb', '500', false, 'Advanced plan: storage per workspace in GB (0 is unlimited)'),
  ('plan_advanced_ai_usd', '15', false, 'Advanced plan: AI test spend a month in USD, across all the account''s workspaces (0 is none)')
on conflict (key) do nothing;
