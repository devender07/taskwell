-- Beta mode (docs/beta-mode-plan.md): the user API hides billing and upgrade wording while it's true.
insert into system_config (key, value, secret, description) values
  ('beta_mode', 'false', false, 'Beta: hide pricing, billing and upgrade prompts, and show the beta notice instead of the privacy notice and terms (true or false)')
on conflict (key) do nothing;
