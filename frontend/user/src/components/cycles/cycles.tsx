import { extractDataFromArray } from '@taskwell/shared/list'
import { useNavigate, useRouter } from '@tanstack/react-router'
import { useActionState, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Dialog } from '@taskwell/shared/dialog'
import { Input } from '@taskwell/shared/input'
import { createCycle, deleteCycle, toFormErrors, updateCycle, type Cycle, type CycleRef, type FieldErrors, type Project, type Workspace } from '@/api'
import { reloadPage } from '@/realtime'
import { FormError } from '@/components/auth/auth-card'
import '@/components/home/home.css'

type State = { errors?: FieldErrors; error?: string | null }

export const STATE_LABEL: Record<CycleRef['state'], string> = { current: 'Current', upcoming: 'Upcoming', past: 'Past' }

// ponytail: UTC dates to match the API's cycle state; use the viewer's timezone if that confuses people.
const todayUTC = () => new Date().toISOString().slice(0, 10)
const addDays = (date: string, n: number) => {
  const d = new Date(`${date}T00:00:00Z`)
  d.setUTCDate(d.getUTCDate() + n)
  return d.toISOString().slice(0, 10)
}

export function CycleProgress({ cycle }: { cycle: Cycle }) {
  const { total, completed } = cycle.progress
  return (
    <span className="grid gap-1" style={{ minWidth: 120 }}>
      <span
        role="progressbar"
        aria-label={`${cycle.name} progress`}
        aria-valuemin={0}
        aria-valuemax={total}
        aria-valuenow={completed}
        aria-valuetext={`${completed} of ${total} done`}
        className="home-bar"
      >
        <span style={{ width: total ? `${(completed / total) * 100}%` : 0 }} />
      </span>
      <span className="text-caption text-secondary">
        {completed} of {total} done
      </span>
    </span>
  )
}

type FormProps = { workspace: Workspace; project: Project; cycles: Cycle[]; cycle?: Cycle; onDone: () => void }

// Create (no cycle) or edit a cycle. A new one starts the day after the latest cycle ends (not before today) and runs two weeks.
export function CycleForm({ workspace, project, cycles, cycle, onDone }: FormProps) {
  const router = useRouter()
  const latest = cycles.reduce<string | null>((max, c) => (max && max > c.ends_on ? max : c.ends_on), null)
  const today = todayUTC()
  const start = latest && addDays(latest, 1) > today ? addDays(latest, 1) : today
  const [name, setName] = useState(extractDataFromArray(cycle, 'custom_name', ''))
  const [startsOn, setStartsOn] = useState(extractDataFromArray(cycle, 'starts_on', start))
  const [endsOn, setEndsOn] = useState(cycle?.ends_on ?? addDays(start, 13))
  const next = Math.max(0, ...cycles.map((c) => c.number)) + 1

  const [{ errors, error }, submit, pending] = useActionState(async (): Promise<State> => {
    const errors = {
      name: name.trim().length > 60 ? 'Use 60 characters or fewer.' : null,
      starts_on: startsOn ? null : 'Choose a start date.',
      ends_on: !endsOn ? 'Choose an end date.' : startsOn && endsOn < startsOn ? "The end date can't be before the start date." : null,
    }
    if (Object.values(errors).some(Boolean)) return { errors }
    const form = new FormData()
    form.set('name', name.trim())
    form.set('starts_on', startsOn)
    form.set('ends_on', endsOn)
    try {
      await (cycle
        ? updateCycle(workspace.slug, project.key, cycle.number, form)
        : createCycle(workspace.slug, project.key, form))
    } catch (e) {
      return toFormErrors(e)
    }
    await reloadPage(router)
    onDone()
    return {}
  }, {})

  return (
    <form action={submit} noValidate className="grid gap-4">
      <Input
        label="Name"
        placeholder={`Cycle ${extractDataFromArray(cycle, 'number', next)}`}
        autoComplete="off"
        autoFocus
        value={name}
        onChange={(e) => setName(e.target.value)}
        error={errors?.name}
      />
      <div className="grid gap-4" style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(160px, 1fr))' }}>
        <Input label="Start date" type="date" value={startsOn} onChange={(e) => setStartsOn(e.target.value)} error={errors?.starts_on} />
        <Input label="End date" type="date" value={endsOn} onChange={(e) => setEndsOn(e.target.value)} error={errors?.ends_on} />
      </div>
      <FormError>{error}</FormError>
      <Button type="submit" className="w-full" disabled={pending}>
        {cycle ? (pending ? 'Saving…' : 'Save changes') : pending ? 'Creating cycle…' : 'Create cycle'}
      </Button>
    </form>
  )
}

export function DeleteCycle({ workspace, project, cycle, open, onClose }: { workspace: Workspace; project: Project; cycle: Cycle; open: boolean; onClose: () => void }) {
  const navigate = useNavigate()
  const [error, remove, pending] = useActionState(async () => {
    try {
      await deleteCycle(workspace.slug, project.key, cycle.number)
    } catch (e) {
      return toFormErrors(e).error
    }
    await navigate({ to: '/$workspace/projects/$key/cycles', params: { workspace: workspace.slug, key: project.key }, replace: true })
    return null
  }, null)

  return (
    <Dialog open={open} onClose={onClose} title={`Delete ${cycle.name}?`} description="Its issues stay in the project.">
      <form action={remove} className="grid gap-4">
        <FormError>{error}</FormError>
        <Button type="submit" variant="danger" className="w-full" disabled={pending}>
          {pending ? 'Deleting…' : 'Delete cycle'}
        </Button>
      </form>
    </Dialog>
  )
}
