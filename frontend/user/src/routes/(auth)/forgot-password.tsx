import { Link, createFileRoute } from '@tanstack/react-router'
import { redirectIfSignedIn } from '@/api'
import { AuthCard } from '@/components/auth/auth-card'
import { ForgotPasswordForm } from '@/components/auth/forgot-password-form'

export const Route = createFileRoute('/(auth)/forgot-password')({
  beforeLoad: redirectIfSignedIn,
  component: ForgotPassword,
})

function ForgotPassword() {
  return (
    <AuthCard
      title="Reset your password"
      subtitle="Enter your email and we'll send you a link to reset it."
      footer={
        <>
          Remembered it? <Link to="/signin" search={{ method: 'password' }} className="auth-link">Back to sign in</Link>
        </>
      }
    >
      <ForgotPasswordForm />
    </AuthCard>
  )
}
