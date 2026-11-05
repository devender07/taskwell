import { Link, createFileRoute } from '@tanstack/react-router'
import { redirectIfSignedIn, safeRedirect } from '@/api'
import { AuthCard, GoogleButton, OrDivider } from '@/components/auth/auth-card'
import { SignupForm } from '@/components/auth/signup-form'

export const Route = createFileRoute('/(auth)/signup')({
  validateSearch: (search): { redirect?: string } => {
    const redirect = safeRedirect(search.redirect)
    return redirect ? { redirect } : {}
  },
  beforeLoad: redirectIfSignedIn,
  component: SignUp,
})

function SignUp() {
  const { redirect } = Route.useSearch()
  return (
    <AuthCard
      title="Create your account"
      subtitle="Set up Taskwell for you and your team."
      footer={
        <>
          Already have an account? <Link to="/signin" search={redirect ? { redirect } : {}} className="auth-link">Sign in</Link>
        </>
      }
    >
      <GoogleButton />
      <OrDivider />
      <SignupForm redirect={redirect} />
    </AuthCard>
  )
}
