-- Starter labels and an "In review" status for new workspaces (docs/workspace-defaults-plan.md). Skips what's there,
-- so admin edits made before this ran are kept.
insert into default_labels (name, color) values
  ('Bug', '#eb5757'), ('Feature', '#9b51e0'), ('Improvement', '#5e6ad2'), ('Docs', '#8a8f98')
on conflict do nothing;

update default_statuses set position = position + 1
where position >= (select position from default_statuses where lower(name) = 'done')
  and not exists (select 1 from default_statuses where lower(name) = 'in review');
insert into default_statuses (name, category, position)
select 'In review', 'started', position - 1 from default_statuses
where lower(name) = 'done' and not exists (select 1 from default_statuses where lower(name) = 'in review');
