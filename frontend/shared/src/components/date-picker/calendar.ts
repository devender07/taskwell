// Dates are plain 'YYYY-MM-DD' strings in local time; '' means no date.

export const pad = (n: number) => String(n).padStart(2, '0')

export function toISO(d: Date) {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

// null for '' or anything that isn't a real calendar date.
export function parseISO(s: string): Date | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(s)
  if (!m) return null
  const d = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]))
  return toISO(d) === s ? d : null
}

export function addDays(iso: string, n: number) {
  const d = parseISO(iso)!
  d.setDate(d.getDate() + n)
  return toISO(d)
}

// Same day in a month n months away, clamped to that month's length (Jan 31 + 1 month = Feb 28).
export function addMonths(iso: string, n: number) {
  const d = parseISO(iso)!
  const first = new Date(d.getFullYear(), d.getMonth() + n, 1)
  const last = new Date(first.getFullYear(), first.getMonth() + 1, 0).getDate()
  return toISO(new Date(first.getFullYear(), first.getMonth(), Math.min(d.getDate(), last)))
}

// The 6 weeks shown for the month containing iso, Monday first.
export function monthGrid(iso: string): string[] {
  const d = parseISO(iso)!
  const first = new Date(d.getFullYear(), d.getMonth(), 1)
  const start = new Date(first)
  start.setDate(1 - ((first.getDay() + 6) % 7))
  return Array.from({ length: 42 }, (_, i) => toISO(new Date(start.getFullYear(), start.getMonth(), start.getDate() + i)))
}

export const sameMonth = (a: string, b: string) => a.slice(0, 7) === b.slice(0, 7)
