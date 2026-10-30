import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useActionState } from 'react'
import { Button } from '@tether/shared/button'
import { api, toFormErrors } from '@/api'

export const Route = createFileRoute('/_app/')({
  component: Dashboard,
})

function Dashboard() {
  const { user } = Route.useRouteContext()
  const navigate = useNavigate()
  const [error, signOut, pending] = useActionState(async () => {
    try {
      await api('/auth/signout', new FormData())
    } catch (e) {
      return toFormErrors(e).error
    }
    await navigate({ to: '/signin' })
    return null
  }, null)

  return (
    <main className="grid place-center min-h-screen p-4">
      <form action={signOut} className="grid justify-items-center gap-4">
        <h1 className="text-h2">Hi {user.name}</h1>
        {error ? <p role="alert" className="text-caption text-accent">{error}</p> : null}
        <Button type="submit" variant="secondary" disabled={pending}>
          {pending ? 'Signing out…' : 'Sign out'}
        </Button>
      </form>
    </main>
  )
}
