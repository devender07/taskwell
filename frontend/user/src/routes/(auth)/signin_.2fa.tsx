import { Link, createFileRoute } from '@tanstack/react-router'
import { redirectIfSignedIn, safeRedirect } from '@/api'
import { AuthCard } from '@/components/auth/auth-card'
import { MfaForm } from '@/components/auth/mfa-form'

// Reached after a password, email-code or Google sign-in when the account has two-factor on.
export const Route = createFileRoute('/(auth)/signin_/2fa')({
  beforeLoad: redirectIfSignedIn,
  validateSearch: (search): { redirect?: string } => {
    const redirect = safeRedirect(search.redirect)
    return redirect ? { redirect } : {}
  },
  component: TwoFactor,
})

function TwoFactor() {
  const { redirect } = Route.useSearch()
  return (
    <AuthCard
      title="Two-factor sign-in"
      subtitle="Enter the 6-digit code from your authenticator app."
      footer={
        <Link to="/signin" search={redirect ? { redirect } : {}} className="auth-link">
          Back to sign in
        </Link>
      }
    >
      <MfaForm redirect={redirect} />
    </AuthCard>
  )
}
