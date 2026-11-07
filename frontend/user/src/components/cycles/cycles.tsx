import { extractDataFromArray } from '@taskwell/shared/list'
import { useNavigate, useRouter } from '@tanstack/react-router'
import { useActionState, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Dialog } from '@taskwell/shared/dialog'
import { Input } from '@taskwell/shared/input'
import { createCycle, deleteCycle, toFormErrors, updateCycle, type Burndown, type Cycle, type CycleRef, type FieldErrors, type Project, type Workspace } from '@/api'
import { reloadPage } from '@/realtime'
import { FormError } from '@/components/auth/auth-card'
import { barPercent } from '@/components/analytics/analytics'
import { formatDate, points } from '@/components/issues/fields'
import { burndownSeries, velocity } from './points'
import '@/components/home/home.css'
import '@/components/analytics/analytics.css'

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

export function PointsDone({ cycle }: { cycle: Cycle }) {
  const { total, completed } = cycle.progress.points
  if (!total) return null
  return (
    <span className="text-caption text-secondary">
      {completed} of {points(total)} done
    </span>
  )
}

const W = 600
const H = 200
const PAD = { left: 32, right: 12, top: 12, bottom: 24 }

export function BurndownChart({ cycle, data }: { cycle: Cycle; data: Burndown }) {
  const { last, remaining, top, empty } = burndownSeries(cycle, data)
  if (empty) return <p className="text-body-sm text-secondary m-0">Add estimates to issues in this cycle to see the burndown.</p>
  const x = (i: number) => PAD.left + (i / last) * (W - PAD.left - PAD.right)
  const y = (v: number) => PAD.top + (1 - v / top) * (H - PAD.top - PAD.bottom)
  const now = remaining.at(-1)
  const summary = `Planned ${points(data.planned ?? 0)}.${now ? ` ${points(now.y)} remaining on ${formatDate(now.date)}.` : ''}`

  return (
    <figure className="grid gap-2 m-0">
      <figcaption className="flex flex-wrap items-center gap-4 text-caption text-secondary">
        <span className="text-label text-primary">Burndown</span>
        <span className="inline-flex items-center gap-2">
          <svg width="16" height="2" aria-hidden><line x1="0" y1="1" x2="16" y2="1" stroke="var(--text-2)" strokeWidth="2" strokeDasharray="4 3" /></svg>
          Ideal
        </span>
        <span className="inline-flex items-center gap-2">
          <svg width="16" height="2" aria-hidden><line x1="0" y1="1" x2="16" y2="1" stroke="var(--text)" strokeWidth="2" /></svg>
          Remaining
        </span>
      </figcaption>
      <p className="sr-only">{summary}</p>
      <svg viewBox={`0 0 ${W} ${H}`} className="w-full h-auto block" aria-hidden style={{ font: '400 12px var(--font)' }}>
        <line x1={PAD.left} x2={W - PAD.right} y1={y(top)} y2={y(top)} stroke="rgba(var(--ink), 0.08)" />
        <line x1={PAD.left} x2={W - PAD.right} y1={y(0)} y2={y(0)} stroke="rgba(var(--ink), 0.16)" />
        <text x={PAD.left - 8} y={y(top)} dy="4" textAnchor="end" fill="var(--text-2)">{top}</text>
        <text x={PAD.left - 8} y={y(0)} dy="4" textAnchor="end" fill="var(--text-2)">0</text>
        <text x={x(0)} y={H - 4} fill="var(--text-2)">{formatDate(cycle.starts_on)}</text>
        <text x={x(last)} y={H - 4} textAnchor="end" fill="var(--text-2)">{formatDate(cycle.ends_on)}</text>
        {data.planned !== null ? (
          <line x1={x(0)} y1={y(data.planned)} x2={x(last)} y2={y(0)} stroke="var(--text-2)" strokeWidth="2" strokeDasharray="6 4" />
        ) : null}
        <polyline points={remaining.map((r) => `${x(r.x)},${y(r.y)}`).join(' ')} fill="none" stroke="var(--text)" strokeWidth="2" strokeLinejoin="round" />
        {remaining.map((r) => (
          <g key={r.date} style={{ pointerEvents: 'all' }}>
            <title>{`${formatDate(r.date)}: ${points(r.y)} remaining`}</title>
            <circle cx={x(r.x)} cy={y(r.y)} r="12" fill="transparent" />
            <circle cx={x(r.x)} cy={y(r.y)} r="4" fill="var(--text)" stroke="var(--surface)" strokeWidth="2" />
          </g>
        ))}
      </svg>
    </figure>
  )
}

export function Velocity({ cycles }: { cycles: Cycle[] }) {
  const v = velocity(cycles)
  if (!v) return null
  const max = Math.max(...v.shown.map((c) => c.progress.points.completed))
  return (
    <section className="grid gap-3 p-6 bg-surface border rounded-lg" aria-label="Velocity">
      <p className="text-label m-0">Velocity: {points(v.average)} per cycle</p>
      <div className="grid gap-1" style={{ maxWidth: 320 }}>
        <div role="group" aria-label="Completed points per past cycle" className="analytics-bars" style={{ height: 64 }}>
          {v.shown.map((c) => {
            const tip = `${c.name}: ${points(c.progress.points.completed)}`
            return (
              <span key={c.id} role="img" tabIndex={0} aria-label={tip} data-tip={tip} className="analytics-bar">
                <span style={{ height: `${barPercent(c.progress.points.completed, max)}%` }} />
              </span>
            )
          })}
        </div>
        <div className="flex justify-between text-caption text-secondary" aria-hidden>
          <span className="ellipsis">{v.shown[0]!.name}</span>
          {v.shown.length > 1 ? <span className="ellipsis">{v.shown.at(-1)!.name}</span> : null}
        </div>
      </div>
    </section>
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
