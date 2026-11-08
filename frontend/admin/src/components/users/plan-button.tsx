import { useRouter } from '@tanstack/react-router'
import { useActionState, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Checkbox } from '@taskwell/shared/checkbox'
import { Dialog } from '@taskwell/shared/dialog'
import { api, PLANS, toFormErrors, type UserRow } from '@/api'

export function PlanButton({ user }: { user: UserRow }) {
  const router = useRouter()
  const [open, setOpen] = useState(false)

  const [error, save, pending] = useActionState(async (_prev: string | null, form: FormData) => {
    form.set('comped', form.get('comped') ? 'true' : 'false')
    try {
      await api(`/users/${encodeURIComponent(user.id)}/plan`, form)
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
        Set plan
      </Button>
      <Dialog
        open={open}
        onClose={() => setOpen(false)}
        title={`Set ${user.name}'s plan`}
        description="A comped plan stays until you clear it; Paddle events leave it alone. Clearing a comp returns the account to its paid plan, or Starter."
      >
        <form action={save} className="grid gap-4">
          <fieldset className="grid gap-1 m-0 p-0" style={{ border: 0 }}>
            <legend className="text-label text-primary mb-2 p-0">Plan</legend>
            {Object.entries(PLANS).map(([id, name]) => (
              <label key={id} className="checkbox" style={{ minHeight: 44 }}>
                <input type="radio" name="plan" value={id} defaultChecked={user.plan === id} />
                {name}
              </label>
            ))}
          </fieldset>
          <Checkbox name="comped" label="Comped" defaultChecked={user.comped} />
          {error ? <p role="alert" className="text-caption text-accent">{error}</p> : null}
          <div className="flex justify-end gap-3">
            <Button variant="ghost" onClick={() => setOpen(false)}>Cancel</Button>
            <Button type="submit" disabled={pending}>
              {pending ? 'Saving…' : 'Save'}
            </Button>
          </div>
        </form>
      </Dialog>
    </>
  )
}
