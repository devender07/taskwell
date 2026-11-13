import { useNavigate } from '@tanstack/react-router'
import { useActionState } from 'react'
import { Button } from '@taskwell/shared/button'
import { useToast } from '@taskwell/shared/toast'
import { acceptMyInvite, api, homePath, signOut, toFormErrors, type Workspace } from '@/api'
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
    await navigate({ to: '/$workspace', params: { workspace: workspace.slug }, replace: true })
    return null
  }, null)

  return (
    <form action={join} className="grid gap-4">
      <FormError>{error}</FormError>
      <Button type="submit" className="w-full" disabled={pending} autoFocus>
        {pending ? 'Joining…' : 'Accept invite'}
      </Button>
      <Button variant="ghost" className="w-full" disabled={pending} onClick={async () => navigate({ href: await homePath() })}>
        Not now
      </Button>
    </form>
  )
}

// Accepts one of the signed-in user's pending invites and opens its workspace (the navigation reloads the workspace
// list). Resolves to the error message, or null once it navigated.
export function useAcceptMyInvite() {
  const navigate = useNavigate()
  return async (id: string) => {
    let workspace: Workspace
    try {
      workspace = await acceptMyInvite(id)
    } catch (e) {
      return toFormErrors(e).error
    }
    await navigate({ to: '/$workspace', params: { workspace: workspace.slug } })
    return null
  }
}

// "Join" on a Taskwell bot invite row; errors show as a toast.
export function JoinInviteButton({ id, onJoin }: { id: string; onJoin?: () => void }) {
  const { show } = useToast()
  const accept = useAcceptMyInvite()
  const [, join, pending] = useActionState(async () => {
    const error = await accept(id)
    if (error) show({ message: error })
    else onJoin?.()
    return null
  }, null)
  return (
    <form action={join} className="flex-noshrink">
      <Button type="submit" variant="secondary" disabled={pending}>
        {pending ? 'Joining…' : 'Join'}
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
