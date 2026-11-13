// Labels for account_events actions (the user's Security activity and the admin user page).
export const ACCOUNT_EVENTS: Record<string, string> = {
  sign_in: 'Signed in',
  sign_in_failed: 'Failed sign-in',
  sign_out: 'Signed out',
  password_changed: 'Password changed',
  password_added: 'Password added',
  email_changed: 'Email changed',
  mfa_enabled: 'Two-factor turned on',
  mfa_disabled: 'Two-factor turned off',
  token_created: 'API token created',
  token_deleted: 'API token deleted',
  reauth: 'Confirmed it was you',
  reauth_failed: 'Failed to confirm it was you',
}
