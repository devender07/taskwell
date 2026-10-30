import { Link, createFileRoute, useNavigate } from '@tanstack/react-router'
import { Tabs } from '@tether/shared/tabs'
import { redirectIfSignedIn } from '@/api'
import { AuthCard, FormError, GoogleButton, OrDivider } from '@/components/auth/auth-card'
import { OtpForm } from '@/components/auth/otp-form'
import { PasswordForm } from '@/components/auth/password-form'

type Method = 'otp' | 'password'
type GoogleError = 'google' | 'google_unavailable'

const googleErrors: Record<GoogleError, string> = {
  google: "Google sign-in didn't work. Try again or use email.",
  google_unavailable: "Google sign-in isn't set up yet. Use email for now.",
}

export const Route = createFileRoute('/(auth)/signin')({
  beforeLoad: redirectIfSignedIn,
  // `reset` is set by the reset-password page to show a one-off success note; `error` by the Google callback.
  validateSearch: (search): { method?: Method; reset?: true; error?: GoogleError } => ({
    ...(search.method === 'password' && { method: 'password' }),
    ...(search.reset === true && { reset: true }),
    ...((search.error === 'google' || search.error === 'google_unavailable') && { error: search.error }),
  }),
  component: SignIn,
})

const methods: { value: Method; label: string }[] = [
  { value: 'otp', label: 'Email code' },
  { value: 'password', label: 'Password' },
]

function SignIn() {
  const { method = 'otp', reset, error } = Route.useSearch()
  const navigate = useNavigate({ from: Route.fullPath })

  return (
    <AuthCard
      title="Sign in to Tether"
      subtitle="Welcome back. Choose how you'd like to continue."
      footer={
        <>
          Don't have an account? <Link to="/signup" className="auth-link">Sign up</Link>
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
        onChange={(m) => navigate({ search: m === 'password' ? { method: m } : {}, replace: true })}
      />
      {method === 'otp' ? <OtpForm /> : <PasswordForm />}
    </AuthCard>
  )
}
