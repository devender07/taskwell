-- Changing a signed-in account's email: a code to the current address and one to the new one, both carrying it.
alter table auth_tokens add column new_email text;
alter table auth_tokens drop constraint auth_tokens_purpose_check,
  add constraint auth_tokens_purpose_check check (purpose in
    ('signin_code', 'password_reset', 'email_verify', 'password_set', 'email_change_old', 'email_change_new'));
