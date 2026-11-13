import { useRouter } from '@tanstack/react-router'
import { useActionState, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Dialog } from '@taskwell/shared/dialog'
import { Input } from '@taskwell/shared/input'
import { api, toFormErrors, type FieldErrors } from '@/api'

type State = { errors?: FieldErrors; error?: string | null; done?: boolean }

export function ResetCard() {
  const router = useRouter()
  const [open, setOpen] = useState(false)
  const [word, setWord] = useState('')
  const [state, reset, pending] = useActionState(async (): Promise<State> => {
    const form = new FormData()
    form.set('confirm', word)
    try {
      await api('/dev/reset', form)
    } catch (e) {
      return toFormErrors(e)
    }
    setOpen(false)
    setWord('')
    await router.invalidate()
    return { done: true }
  }, {})

  return (
    <section className="flex flex-wrap items-center justify-between gap-4 p-5 bg-surface border rounded-lg" aria-label="Local database">
      <div className="grid gap-1" style={{ maxWidth: 640 }}>
        <h2 className="text-label m-0">Delete user data <span className="text-secondary">· local only</span></h2>
        <p className="text-caption text-secondary m-0">
          Deletes every user, workspace and what's in them. Settings, admins, the admin audit log and workspace defaults stay. Not available in production.
        </p>
        {state.done ? <p role="status" className="text-caption text-success m-0">User data deleted.</p> : null}
      </div>
      <Button variant="danger" onClick={() => setOpen(true)}>
        Delete user data
      </Button>
      <Dialog open={open} onClose={() => setOpen(false)} title="Delete all user data?" description="This can't be undone. Type delete to confirm.">
        <form action={reset} noValidate className="grid gap-4">
          <Input label="Type delete to confirm" autoComplete="off" value={word} onChange={(e) => setWord(e.target.value)} error={state.errors?.confirm ?? state.error} />
          <div className="flex justify-end gap-3">
            <Button variant="ghost" onClick={() => setOpen(false)} disabled={pending}>
              Cancel
            </Button>
            <Button type="submit" variant="danger" disabled={pending || word.trim().toLowerCase() !== 'delete'}>
              {pending ? 'Deleting…' : 'Delete user data'}
            </Button>
          </div>
        </form>
      </Dialog>
    </section>
  )
}
