import { Link, createFileRoute } from '@tanstack/react-router'
import { Button } from '@tether/shared/button'
import { redirectIfSignedIn } from '@/api'
import { AuthCard, OrDivider } from '@/components/auth/auth-card'
import { SignupForm } from '@/components/auth/signup-form'

export const Route = createFileRoute('/(auth)/signup')({
  beforeLoad: redirectIfSignedIn,
  component: SignUp,
})

function SignUp() {
  return (
    <AuthCard
      title="Create your account"
      subtitle="Set up Tether for you and your team."
      footer={
        <>
          Already have an account? <Link to="/signin" className="auth-link">Sign in</Link>
        </>
      }
    >
      <Button variant="secondary" className="w-full">
        Continue with Google
      </Button>
      <OrDivider />
      <SignupForm />
    </AuthCard>
  )
}
