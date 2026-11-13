import { DAY } from '../../../../shared/src/time/relative.ts'
import type { Burndown, Cycle } from '../../api.ts'

// Up to `n` of the latest past cycles, oldest first.
export const latestPast = (cycles: Cycle[], n: number) =>
  cycles
    .filter((c) => c.state === 'past')
    .sort((a, b) => b.ends_on.localeCompare(a.ends_on))
    .slice(0, n)
    .reverse()

// Average completed points of the last 3 past cycles; null when the last 6 have no estimates (nothing to chart).
export function velocity(cycles: Cycle[]) {
  const shown = latestPast(cycles, 6)
  if (!shown.some((c) => c.progress.points.total > 0)) return null
  const last = shown.slice(-3)
  return { average: Math.round(last.reduce((sum, c) => sum + c.progress.points.completed, 0) / last.length), shown }
}

export const dayIndex = (start: string, date: string) => Math.round((Date.parse(date) - Date.parse(start)) / DAY)

// Days of the cycle (0 = starts_on), remaining points per recorded day, and the y-axis top.
export function burndownSeries(cycle: Pick<Cycle, 'starts_on' | 'ends_on'>, b: Burndown) {
  const last = Math.max(1, dayIndex(cycle.starts_on, cycle.ends_on))
  const remaining = b.days.map((d) => ({ date: d.date, x: dayIndex(cycle.starts_on, d.date), y: Math.max(0, d.scope - d.done) }))
  const top = Math.max(1, b.planned ?? 0, ...remaining.map((r) => r.y))
  return { last, remaining, top, empty: b.planned === null && !b.days.some((d) => d.scope > 0) }
}
