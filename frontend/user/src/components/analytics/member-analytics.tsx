import { Link, useRouter } from '@tanstack/react-router'
import { useEffect } from 'react'
import { CategoryGlyph } from '@taskwell/shared/icons'
import type { AnalyticsWeek, MemberAnalytics } from '@/api'
import { reloadRoutes } from '@/realtime'
import { Card, Empty } from '@/components/home/blocks'
import { categoryLabel } from '@/components/issues/fields'
import { barPercent, days, weekLabel } from './analytics'
import './analytics.css'

export function useRefetchEvery(routeId: string, ms = 60_000) {
  const router = useRouter()
  useEffect(() => {
    const timer = setInterval(() => document.visibilityState === 'visible' && reloadRoutes(router, [routeId]), ms)
    return () => clearInterval(timer)
  }, [router, routeId, ms])
}

const CATEGORIES = [
  { key: 'started', color: 'var(--text)' },
  { key: 'unstarted', color: 'var(--text-2)' },
  { key: 'backlog', color: 'rgba(var(--ink), 0.24)' },
] as const

function WeekBars({ weeks, value, noun, color, height, label, grow }: { weeks: AnalyticsWeek[]; value: (w: AnalyticsWeek) => number; noun: string; color: string; height: number; label: string; grow?: boolean }) {
  const max = Math.max(...weeks.map(value))
  return (
    <div className={`flex flex-col gap-1 ${grow ? 'flex-grow' : ''}`}>
      <div role="group" aria-label={label} className={`analytics-bars ${grow ? 'flex-grow' : ''}`} style={{ minHeight: height }}>
        {weeks.map((w) => {
          const tip = `Week of ${weekLabel(w.week)}: ${value(w)} ${noun}`
          return (
            <span key={w.week} role="img" tabIndex={0} aria-label={tip} data-tip={tip} className="analytics-bar">
              <span style={{ height: `${barPercent(value(w), max)}%`, ['--bar' as string]: color }} />
            </span>
          )
        })}
      </div>
      {weeks.length ? (
        <div className="flex justify-between text-caption text-secondary" aria-hidden>
          <span>{weekLabel(weeks[0]!.week)}</span>
          <span>{weekLabel(weeks.at(-1)!.week)}</span>
        </div>
      ) : null}
    </div>
  )
}

function Stat({ value, label, className = '' }: { value: number | string; label: string; className?: string }) {
  return (
    <div className="grid bg-surface border rounded-lg px-6 py-4" style={{ minWidth: 0 }}>
      <span className={`text-h2 ${className}`}>{value}</span>
      <span className="text-caption text-secondary">{label}</span>
    </div>
  )
}

function Stats({ data }: { data: MemberAnalytics }) {
  return (
    <div className="analytics-stats">
      <Stat value={data.open.total} label="Open" />
      <Stat value={data.open.overdue} label="Overdue" className={data.open.overdue ? 'text-accent' : ''} />
      <Stat value={data.open.due_this_week} label="Due this week" />
      <Stat value={data.completed_30d} label="Completed in 30 days" className={data.completed_30d ? 'text-success' : ''} />
      <Stat value={data.cycle_time_days === null ? '–' : days(data.cycle_time_days)} label="Median cycle time" />
      <Stat value={data.tests.runs_started_30d} label="Test runs in 30 days" />
    </div>
  )
}

function Workload({ open }: { open: MemberAnalytics['open'] }) {
  return (
    <Card title="Workload" action={<span className="text-caption text-secondary">{open.total} open</span>}>
      {open.total ? (
        <div className="grid gap-4 px-6 py-4">
          <div className="analytics-stack" aria-hidden>
            {CATEGORIES.map((c) =>
              open.by_category[c.key] ? <span key={c.key} style={{ flex: open.by_category[c.key], background: c.color }} /> : null,
            )}
          </div>
          <ul className="home-list">
            {CATEGORIES.map((c) => (
              <li key={c.key} className="flex items-center gap-3 py-3 text-body-sm">
                <CategoryGlyph category={c.key} color={c.color} />
                <span className="flex-grow text-secondary">{categoryLabel(c.key)}</span>
                <span className="text-primary">{open.by_category[c.key]}</span>
                <span className="text-caption text-secondary text-right" style={{ width: 40 }}>{Math.round((open.by_category[c.key] / open.total) * 100)}%</span>
              </li>
            ))}
          </ul>
        </div>
      ) : (
        <Empty text="No open issues." />
      )}
    </Card>
  )
}

function Completed({ data }: { data: MemberAnalytics }) {
  const total = data.weeks.reduce((sum, w) => sum + w.completed, 0)
  return (
    <Card title="Completed per week" action={<span className="text-caption text-secondary">{total} in 12 weeks</span>}>
      <div className="flex flex-col flex-grow gap-4 px-6 py-4">
        <WeekBars weeks={data.weeks} value={(w) => w.completed} noun="completed" color="var(--success)" height={96} label="Issues completed per week" grow />
      </div>
    </Card>
  )
}

function Activity({ weeks }: { weeks: AnalyticsWeek[] }) {
  const rows = [
    { label: 'Issues created', noun: 'created', value: (w: AnalyticsWeek) => w.created },
    { label: 'Comments', noun: 'comments', value: (w: AnalyticsWeek) => w.comments },
  ]
  return (
    <Card title="Activity per week">
      <div className="grid gap-4 px-6 py-4">
        {rows.map((r) => {
          const total = weeks.reduce((sum, w) => sum + r.value(w), 0)
          return (
            <div key={r.label} className="grid gap-2">
              <div className="flex justify-between text-caption">
                <span className="text-secondary">{r.label}</span>
                <span className="text-primary">{total} in 12 weeks</span>
              </div>
              <WeekBars weeks={weeks} value={r.value} noun={r.noun} color="var(--text-2)" height={40} label={`${r.label} per week`} />
            </div>
          )
        })}
      </div>
    </Card>
  )
}

function Cycles({ workspace, cycles }: { workspace: string; cycles: MemberAnalytics['current_cycles'] }) {
  return (
    <Card title="Current cycles">
      {cycles.length === 0 ? (
        <Empty text="No issues in a current cycle." />
      ) : (
        <ul className="home-list">
          {cycles.map((c) => (
            <li key={`${c.project.key}-${c.number}`}>
              <Link to="/$workspace/projects/$key/cycles/$number" params={{ workspace, key: c.project.key, number: String(c.number) }} className="home-project">
                <span className="flex items-center gap-3" style={{ minWidth: 0 }}>
                  <span className="text-label text-primary ellipsis flex-grow">{c.name}</span>
                  <span className="issue-id flex-noshrink">{c.project.key}</span>
                </span>
                <span
                  role="progressbar"
                  aria-label={`${c.name} progress`}
                  aria-valuemin={0}
                  aria-valuemax={c.assigned}
                  aria-valuenow={c.completed}
                  aria-valuetext={`${c.completed} of ${c.assigned} done`}
                  className="home-bar"
                >
                  <span style={{ width: c.assigned ? `${(c.completed / c.assigned) * 100}%` : 0 }} />
                </span>
                <span className="text-caption text-secondary">
                  {c.completed} of {c.assigned} done
                </span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </Card>
  )
}

export function MemberCards({ workspace, data }: { workspace: string; data: MemberAnalytics }) {
  return (
    <>
      <Stats data={data} />
      <div className="analytics-grid">
        <Completed data={data} />
        <Activity weeks={data.weeks} />
        <Workload open={data.open} />
        <Cycles workspace={workspace} cycles={data.current_cycles} />
      </div>
    </>
  )
}
