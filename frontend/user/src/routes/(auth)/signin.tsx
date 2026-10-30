import { Link, createFileRoute, useNavigate } from '@tanstack/react-router'
import { Button } from '@tether/shared/button'
import { Tabs } from '@tether/shared/tabs'
import { redirectIfSignedIn } from '@/api'
import { AuthCard, OrDivider } from '@/components/auth/auth-card'
import { OtpForm } from '@/components/auth/otp-form'
import { PasswordForm } from '@/components/auth/password-form'

type Method = 'otp' | 'password'

export const Route = createFileRoute('/(auth)/signin')({
  beforeLoad: redirectIfSignedIn,
  // `reset` is set by the reset-password page to show a one-off success note.
  validateSearch: (search): { method?: Method; reset?: true } => ({
    ...(search.method === 'password' && { method: 'password' }),
    ...(search.reset === true && { reset: true }),
  }),
  component: SignIn,
})

const methods: { value: Method; label: string }[] = [
  { value: 'otp', label: 'Email code' },
  { value: 'password', label: 'Password' },
]

function SignIn() {
  const { method = 'otp', reset } = Route.useSearch()
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
      <Button variant="secondary" className="w-full">
        Continue with Google
      </Button>
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
