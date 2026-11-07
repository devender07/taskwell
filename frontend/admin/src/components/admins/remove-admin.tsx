import { useRouter } from '@tanstack/react-router'
import { useActionState, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Dialog } from '@taskwell/shared/dialog'
import { api, toFormErrors, type AdminAccount } from '@/api'

export function RemoveAdminButton({ admin }: { admin: AdminAccount }) {
  const router = useRouter()
  const [open, setOpen] = useState(false)

  const [error, confirm, pending] = useActionState(async () => {
    try {
      await api(`/admins/${encodeURIComponent(admin.id)}/remove`, new FormData())
    } catch (e) {
      return toFormErrors(e).error
    }
    setOpen(false)
    await router.invalidate()
    return null
  }, null)

  return (
    <>
      <Button variant="ghost" onClick={() => setOpen(true)}>Remove</Button>
      <Dialog
        open={open}
        onClose={() => setOpen(false)}
        title={`Remove ${admin.name}?`}
        description={`${admin.email} is signed out of the admin app right away and can't sign in again. Their app account, if they have one, isn't touched.`}
      >
        <form action={confirm} className="grid gap-4">
          {error ? <p role="alert" className="text-caption text-accent">{error}</p> : null}
          <div className="flex justify-end gap-3">
            <Button variant="ghost" onClick={() => setOpen(false)}>Cancel</Button>
            <Button type="submit" variant="danger" disabled={pending}>
              {pending ? 'Removing…' : 'Remove'}
            </Button>
          </div>
        </form>
      </Dialog>
    </>
  )
}
