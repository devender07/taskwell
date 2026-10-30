import { useRouter } from '@tanstack/react-router'
import { useActionState, useState } from 'react'
import { Button } from '@tether/shared/button'
import { Dialog } from '@tether/shared/dialog'
import { api, toFormErrors, type UserRow } from '@/api'

export function SuspendButton({ user }: { user: UserRow }) {
  const router = useRouter()
  const [open, setOpen] = useState(false)
  const action = user.suspended ? 'unsuspend' : 'suspend'

  const [error, confirm, pending] = useActionState(async () => {
    try {
      await api(`/users/${encodeURIComponent(user.id)}/${action}`, new FormData())
    } catch (e) {
      return toFormErrors(e).error
    }
    setOpen(false)
    await router.invalidate()
    return null
  }, null)

  return (
    <>
      <Button variant="ghost" onClick={() => setOpen(true)}>
        {user.suspended ? 'Unsuspend' : 'Suspend'}
      </Button>
      <Dialog
        open={open}
        onClose={() => setOpen(false)}
        title={user.suspended ? `Unsuspend ${user.name}?` : `Suspend ${user.name}?`}
        description={
          user.suspended
            ? `${user.email} will be able to sign in again.`
            : `${user.email} will lose access within minutes and can't sign in until unsuspended.`
        }
      >
        <form action={confirm} className="grid gap-4">
          {error ? <p role="alert" className="text-caption text-accent">{error}</p> : null}
          <div className="flex justify-end gap-3">
            <Button variant="ghost" onClick={() => setOpen(false)}>Cancel</Button>
            <Button type="submit" variant={user.suspended ? 'primary' : 'danger'} disabled={pending}>
              {pending ? 'Saving…' : user.suspended ? 'Unsuspend' : 'Suspend'}
            </Button>
          </div>
        </form>
      </Dialog>
    </>
  )
}
