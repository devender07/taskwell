import type { Day } from '@/api'

const dayLabel = (date: string) => new Date(`${date}T00:00:00`).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })

const count = (n: number) => n.toLocaleString(undefined)

export function BarChart({ title, days, format = count }: { title: string; days: Day[]; format?: (n: number) => string }) {
  const max = Math.max(1, ...days.map((d) => d.count))
  const total = days.reduce((n, d) => n + d.count, 0)
  return (
    <section className="grid gap-4 p-5 bg-surface border rounded-lg">
      <header className="flex items-baseline justify-between gap-3">
        <h3 className="text-label">{title}</h3>
        <span className="text-caption text-secondary">{format(total)} in the last 30 days</span>
      </header>
      <div className="bars" role="img" aria-label={`${title} per day, last 30 days: ${format(total)} total, at most ${format(max)} in a day`}>
        {days.map((d) => (
          <div key={d.date} title={`${dayLabel(d.date)}: ${format(d.count)}`} style={{ height: `${(d.count / max) * 100}%` }} />
        ))}
      </div>
      {days.length > 0 ? (
        <div className="flex justify-between text-caption text-secondary">
          <span>{dayLabel(days[0]!.date)}</span>
          <span>{dayLabel(days[days.length - 1]!.date)}</span>
        </div>
      ) : null}
    </section>
  )
}
