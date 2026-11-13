-- A "Report a bug" link in the user app's sidebar, set from the admin app's Config page.
insert into system_config (key, value, secret, description) values
  ('app_feedback_url', '', false, 'Link for "Report a bug" in the user app sidebar, e.g. your Taskwell feedback form (http or https); empty hides it')
on conflict (key) do nothing;
