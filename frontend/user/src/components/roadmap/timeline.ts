// Roadmap timeline math on 'YYYY-MM-DD' strings, as whole UTC days so DST never shifts a bar.

export type Zoom = 'month' | 'quarter' | 'year'

// Pixels per day, and the days the timeline always covers around today.
export const DAY_PX: Record<Zoom, number> = { month: 32, quarter: 8, year: 3 }
const SPAN: Record<Zoom, number> = { month: 31, quarter: 92, year: 366 }
const DAY_MS = 86_400_000

export const toDay = (iso: string) => Date.UTC(+iso.slice(0, 4), +iso.slice(5, 7) - 1, +iso.slice(8, 10)) / DAY_MS
export const fromDay = (day: number) => new Date(day * DAY_MS).toISOString().slice(0, 10)
const monthStart = (iso: string, add = 0) => {
  const d = new Date(Date.UTC(+iso.slice(0, 4), +iso.slice(5, 7) - 1 + add, 1))
  return d.toISOString().slice(0, 10)
}

export type Timeline = { start: number; days: number; dayPx: number }

// From the month before the earliest date to the end of the month after the latest, and never less than the zoom's
// span centred on today.
export function timeline(dates: string[], today: string, zoom: Zoom): Timeline {
  const t = toDay(today)
  const half = Math.ceil(SPAN[zoom] / 2)
  let min = t - half
  let max = t + half
  for (const d of dates) {
    const n = toDay(d)
    if (n < min) min = n
    if (n > max) max = n
  }
  const start = toDay(monthStart(fromDay(min), -1))
  const end = toDay(monthStart(fromDay(max), 2)) // exclusive
  return { start, days: end - start, dayPx: DAY_PX[zoom] }
}

// Left edge of a day, and the width of an inclusive range of days.
export const xOf = (tl: Timeline, iso: string) => (toDay(iso) - tl.start) * tl.dayPx
export const widthOf = (tl: Timeline, from: string, to: string) => (toDay(to) - toDay(from) + 1) * tl.dayPx

export type Mark = { x: number; label: string; major: boolean }

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

// Month labels for the header, with the year on January and on the first month shown.
export function months(tl: Timeline): Mark[] {
  const out: Mark[] = []
  for (let iso = fromDay(tl.start); toDay(iso) < tl.start + tl.days; iso = monthStart(iso, 1)) {
    const m = +iso.slice(5, 7) - 1
    const label = m === 0 || !out.length ? `${MONTHS[m]} ${iso.slice(0, 4)}` : MONTHS[m]!
    out.push({ x: xOf(tl, iso), label, major: true })
  }
  return out
}

// Gridlines and their small labels: days (month zoom), Mondays (quarter), or month starts (year, unlabelled).
export function ticks(tl: Timeline, zoom: Zoom): Mark[] {
  if (zoom === 'year') return months(tl).map((m) => ({ ...m, label: '' }))
  const out: Mark[] = []
  for (let day = tl.start; day < tl.start + tl.days; day++) {
    const iso = fromDay(day)
    const first = iso.endsWith('-01')
    if (zoom === 'quarter' && new Date(day * DAY_MS).getUTCDay() !== 1) continue
    out.push({ x: (day - tl.start) * tl.dayPx, label: String(+iso.slice(8)), major: first })
  }
  return out
}

// Share of the project's work that's done; canceled issues don't count either way.
export function percentDone(p: { total: number; completed: number; canceled: number }) {
  const open = p.total - p.canceled
  return open > 0 ? Math.round((p.completed / open) * 100) : 0
}
