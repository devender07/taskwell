-- Plan grace (docs/plan-grace-plan.md): a comp can end on its own at plan_comped_until (null: no end, or not comped).
alter table users add column plan_comped_until timestamptz;
create index users_plan_comped_until on users (plan_comped_until) where plan_comped_until is not null;
