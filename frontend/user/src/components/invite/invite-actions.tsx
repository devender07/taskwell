import { useNavigate } from '@tanstack/react-router'
import { useActionState } from 'react'
import { Button } from '@tether/shared/button'
import { api, signOut, toFormErrors, type Workspace } from '@/api'
import { FormError } from '@/components/auth/auth-card'

export function JoinWorkspace({ token }: { token: string }) {
  const navigate = useNavigate()
  const [error, join, pending] = useActionState(async () => {
    let workspace: Workspace
    try {
      workspace = await api<Workspace>(`/invites/${encodeURIComponent(token)}/accept`, new FormData())
    } catch (e) {
      return toFormErrors(e).error
    }
    await navigate({ to: '/$workspace', params: { workspace: workspace.slug } })
    return null
  }, null)

  return (
    <form action={join} className="grid gap-4">
      <FormError>{error}</FormError>
      <Button type="submit" className="w-full" disabled={pending}>
        {pending ? 'Joining…' : 'Join workspace'}
      </Button>
    </form>
  )
}

export function SwitchAccount({ redirect }: { redirect: string }) {
  const navigate = useNavigate()
  const [error, switchAccount, pending] = useActionState(async () => {
    try {
      await signOut()
    } catch (e) {
      return toFormErrors(e).error
    }
    await navigate({ to: '/signin', search: { redirect } })
    return null
  }, null)

  return (
    <form action={switchAccount} className="grid gap-4">
      <FormError>{error}</FormError>
      <Button type="submit" variant="secondary" className="w-full" disabled={pending}>
        {pending ? 'Signing out…' : 'Switch account'}
      </Button>
    </form>
  )
}
