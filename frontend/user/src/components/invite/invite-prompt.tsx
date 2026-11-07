import { useActionState, useState, useSyncExternalStore } from 'react'
import { Button } from '@taskwell/shared/button'
import { Dialog } from '@taskwell/shared/dialog'
import { useToast } from '@taskwell/shared/toast'
import { api, dismissMyInvite, myInvitesPath, toFormErrors, type PendingInvite } from '@/api'
import { FormError } from '@/components/auth/auth-card'
import { subscribePaths } from '@/realtime'
import { useAcceptMyInvite } from './invite-actions'

// The user's pending invites, kept live: one request and one subscription however many components read them.
let invites: PendingInvite[] = []
const readers = new Set<() => void>()
let unsubscribe: (() => void) | null = null
const setInvites = (list: PendingInvite[]) => {
  if (!unsubscribe) return // a late answer after everyone stopped reading
  invites = list
  readers.forEach((r) => r())
}
const loadInvites = () => api<PendingInvite[]>(myInvitesPath).then(setInvites, () => setInvites([]))

function readInvites(onChange: () => void) {
  readers.add(onChange)
  if (!unsubscribe) {
    void loadInvites()
    unsubscribe = subscribePaths([myInvitesPath], loadInvites)
  }
  return () => {
    readers.delete(onChange)
    // A tick later, so a remount right away (a route change, StrictMode) keeps the subscription instead of refetching.
    setTimeout(() => {
      if (readers.size) return
      unsubscribe?.()
      unsubscribe = null
      invites = [] // nobody reads them now (e.g. signed out), so the next account never sees these
    })
  }
}

export const useMyInvites = () => useSyncExternalStore(readInvites, () => invites)

// Offers the oldest pending invite for the user's email. "Not now" (or closing) dismisses it into the Taskwell bot of
// `slug`, the workspace the user is in; the next one shows after.
export function InvitePrompt({ slug }: { slug: string }) {
  const { show } = useToast()
  const accept = useAcceptMyInvite()
  const invites = useMyInvites()
  const [gone, setGone] = useState<string[]>([])
  const invite = invites.find((i) => !i.dismissed && !gone.includes(i.id))
  // Keeps the last invite on screen while the dialog fades out.
  const [shown, setShown] = useState(invite)
  if (invite && invite !== shown) setShown(invite)

  const [error, join, pending] = useActionState(async () => {
    if (!invite) return null
    const error = await accept(invite.id)
    if (!error) setGone((g) => [...g, invite.id])
    return error
  }, null)

  function notNow() {
    if (!invite || pending) return
    setGone((g) => [...g, invite.id])
    dismissMyInvite(invite.id, slug).catch((e) => show({ message: toFormErrors(e).error ?? 'Could not dismiss the invite.' }))
  }

  if (!shown) return null
  const by = shown.invited_by || 'Someone'
  return (
    <Dialog
      open={!!invite}
      onClose={notNow}
      title={`Join ${shown.workspace.name}?`}
      description={
        shown.guest
          ? `${by} invited you as a guest. You'll see only the projects you're added to.`
          : `${by} invited you as ${shown.role.name}.`
      }
    >
      <form action={join} className="grid gap-4">
        <FormError>{error}</FormError>
        <div className="flex justify-end gap-3">
          <Button variant="ghost" onClick={notNow} disabled={pending}>
            Not now
          </Button>
          <Button type="submit" disabled={pending} autoFocus>
            {pending ? 'Joining…' : 'Join workspace'}
          </Button>
        </div>
      </form>
    </Dialog>
  )
}
