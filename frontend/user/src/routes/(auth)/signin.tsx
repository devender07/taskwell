import { Link, createFileRoute, useNavigate } from '@tanstack/react-router'
import { Tabs } from '@taskwell/shared/tabs'
import { redirectIfSignedIn, safeRedirect } from '@/api'
import { AuthCard, FormError, GoogleButton, LegalCaption, OrDivider } from '@/components/auth/auth-card'
import { OtpForm } from '@/components/auth/otp-form'
import { PasswordForm } from '@/components/auth/password-form'

type Method = 'otp' | 'password'
type GoogleError = 'google' | 'google_unavailable' | 'suspended'

const googleErrors: Record<GoogleError, string> = {
  google: "Google sign-in didn't work. Try again or use email.",
  google_unavailable: "Google sign-in isn't set up yet. Use email for now.",
  suspended: 'This account is suspended.',
}

export const Route = createFileRoute('/(auth)/signin')({
  beforeLoad: redirectIfSignedIn,
  // `reset` is set by the reset-password page to show a one-off success note; `error` by the Google callback;
  // `redirect` by the invite page to come back after signing in.
  validateSearch: (search): { method?: Method; reset?: true; error?: GoogleError; redirect?: string } => {
    const redirect = safeRedirect(search.redirect)
    return {
      ...(search.method === 'password' && { method: 'password' }),
      ...(search.reset === true && { reset: true }),
      ...(typeof search.error === 'string' && search.error in googleErrors && { error: search.error as GoogleError }),
      ...(redirect && { redirect }),
    }
  },
  component: SignIn,
})

const methods: { value: Method; label: string }[] = [
  { value: 'otp', label: 'Email code' },
  { value: 'password', label: 'Password' },
]

function SignIn() {
  const { method = 'otp', reset, error, redirect } = Route.useSearch()
  const navigate = useNavigate({ from: Route.fullPath })

  return (
    <AuthCard
      title="Sign in to Taskwell"
      subtitle="Welcome back. Choose how you'd like to continue."
      footer={
        <>
          Don't have an account? <Link to="/signup" search={redirect ? { redirect } : {}} className="auth-link">Sign up</Link>
        </>
      }
    >
      {reset ? (
        <p role="status" className="text-body-sm text-success text-center">
          Password updated. Sign in with your new password.
        </p>
      ) : null}
      {error ? <FormError>{googleErrors[error]}</FormError> : null}
      <GoogleButton />
      <OrDivider />
      <Tabs
        aria-label="Sign-in method"
        items={methods}
        value={method}
        onChange={(m) => navigate({ search: { ...(m === 'password' && { method: m }), ...(redirect && { redirect }) }, replace: true })}
      />
      {method === 'otp' ? <OtpForm redirect={redirect} /> : <PasswordForm redirect={redirect} />}
      <LegalCaption />
    </AuthCard>
  )
}
