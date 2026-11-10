-- Security admin (docs/security-admin-plan.md): workspaces can require two-factor sign-in, and creating an API token
-- or deleting a workspace asks for a recent re-authentication (account events reauth/reauth_failed, emailed code
-- purpose reauth).
alter table workspaces add column require_mfa boolean not null default false;

alter table account_events drop constraint account_events_action_check,
  add constraint account_events_action_check check (action in ('sign_in', 'sign_in_failed', 'sign_out',
    'password_changed', 'password_added', 'email_changed', 'mfa_enabled', 'mfa_disabled', 'token_created',
    'token_deleted', 'reauth', 'reauth_failed'));

alter table auth_tokens drop constraint auth_tokens_purpose_check,
  add constraint auth_tokens_purpose_check check (purpose in
    ('signin_code', 'password_reset', 'email_verify', 'password_set', 'email_change_old', 'email_change_new',
     'account_delete', 'reauth'));
