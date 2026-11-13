import { useRouter } from '@tanstack/react-router'
import { useActionState, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Checkbox } from '@taskwell/shared/checkbox'
import { Dialog } from '@taskwell/shared/dialog'
import { SmartAutocomplete } from '@taskwell/shared/smart-autocomplete'
import { api, PLANS, toFormErrors, type UserRow } from '@/api'

const PLAN_OPTIONS = Object.entries(PLANS).map(([value, label]) => ({ value, label }))
const DAYS = [{ value: '', label: 'No end date' }, ...[7, 14, 30, 90].map((d) => ({ value: String(d), label: `${d} days` }))]

export function PlanButton({ user }: { user: UserRow }) {
  const router = useRouter()
  const [open, setOpen] = useState(false)
  const [comped, setComped] = useState(user.comped)

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
      <Button variant="ghost" onClick={() => { setComped(user.comped); setOpen(true) }}>
        Set plan
      </Button>
      <Dialog
        open={open}
        onClose={() => setOpen(false)}
        title={`Set ${user.name}'s plan`}
        description="A comped plan stays until it ends or you clear it; Paddle events leave it alone. Clearing a comp returns the account to its paid plan, or Free."
      >
        <form action={save} className="grid gap-4">
          <SmartAutocomplete label="Plan" name="plan" options={PLAN_OPTIONS} defaultValue={PLAN_OPTIONS.find((o) => o.value === user.plan) ?? null} />
          <Checkbox name="comped" label="Comped" checked={comped} onChange={(e) => setComped(e.target.checked)} />
          {comped ? <SmartAutocomplete label="For" name="days" options={DAYS} defaultValue={DAYS[0]} /> : null}
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
