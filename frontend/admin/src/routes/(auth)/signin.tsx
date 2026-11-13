import { createFileRoute } from '@tanstack/react-router'
import { LogoMark } from '@taskwell/shared/logo'
import { redirectIfSignedIn, safeRedirect } from '@/api'
import { SigninForm } from '@/components/auth/signin-form'

export const Route = createFileRoute('/(auth)/signin')({
  beforeLoad: redirectIfSignedIn,
  validateSearch: (search): { redirect?: string } => {
    const redirect = safeRedirect(search.redirect)
    return redirect ? { redirect } : {}
  },
  component: SignIn,
})

function SignIn() {
  const { redirect } = Route.useSearch()
  return (
    <main className="grid place-center min-h-screen p-4">
      <div className="grid gap-5 w-full p-8 bg-surface border rounded-lg" style={{ maxWidth: 400 }}>
        <header className="grid justify-items-center gap-2 text-center">
          <span className="mb-2">
            <LogoMark />
          </span>
          <h1 className="text-h2">Taskwell admin</h1>
          <p className="text-body-sm text-secondary">Sign in with your staff account.</p>
        </header>
        <SigninForm redirect={redirect} />
      </div>
    </main>
  )
}
