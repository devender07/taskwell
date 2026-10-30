import { Link, createFileRoute } from '@tanstack/react-router'
import { AuthCard, FormError } from '@/components/auth/auth-card'
import { ResetPasswordForm } from '@/components/auth/reset-password-form'

// No signed-in redirect here: a reset link should work in a browser that already has a session.
export const Route = createFileRoute('/(auth)/reset-password')({
  validateSearch: (search): { token?: string } => (typeof search.token === 'string' && search.token ? { token: search.token } : {}),
  component: ResetPassword,
})

function ResetPassword() {
  const { token } = Route.useSearch()
  return (
    <AuthCard
      title="Choose a new password"
      subtitle="It must be at least 8 characters."
      footer={
        <>
          Need a new link? <Link to="/forgot-password" className="auth-link">Request one</Link>
        </>
      }
    >
      {token ? <ResetPasswordForm token={token} /> : <FormError>This reset link is invalid or expired.</FormError>}
    </AuthCard>
  )
}
