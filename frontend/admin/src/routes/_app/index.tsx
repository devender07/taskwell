import { createFileRoute } from '@tanstack/react-router'
import { api, type Day, type Stats } from '@/api'

export const Route = createFileRoute('/_app/')({
  loader: () => api<Stats>('/stats'),
  component: Overview,
})

function Overview() {
  const { totals, active_users_7d, signups, issues_created } = Route.useLoaderData()
  const tiles = [
    ['Users', totals.users],
    ['Workspaces', totals.workspaces],
    ['Projects', totals.projects],
    ['Issues', totals.issues],
    ['Comments', totals.comments],
    ['Active users, last 7 days', active_users_7d],
  ] as const

  return (
    <main className="grid gap-6 w-full px-6 py-10" style={{ maxWidth: 1040 }}>
      <h1 className="text-h2">Overview</h1>
      <section className="grid gap-4" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(160px, 1fr))' }} aria-label="Totals">
        {tiles.map(([label, value]) => (
          <div key={label} className="grid gap-1 p-5 bg-surface border rounded-lg">
            <span className="text-caption text-secondary">{label}</span>
            <span className="text-h2">{value.toLocaleString('en-US')}</span>
          </div>
        ))}
      </section>
      <div className="grid gap-4" style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(300px, 1fr))' }}>
        <BarChart title="Signups" days={signups} />
        <BarChart title="Issues created" days={issues_created} />
      </div>
    </main>
  )
}

const shortDate = (date: string) => new Date(`${date}T00:00:00`).toLocaleDateString('en-US', { month: 'short', day: 'numeric' })

function BarChart({ title, days }: { title: string; days: Day[] }) {
  const max = Math.max(1, ...days.map((d) => d.count))
  const total = days.reduce((n, d) => n + d.count, 0)
  return (
    <section className="grid gap-4 p-5 bg-surface border rounded-lg">
      <header className="flex items-baseline justify-between gap-3">
        <h2 className="text-label">{title}</h2>
        <span className="text-caption text-secondary">{total.toLocaleString('en-US')} in the last 30 days</span>
      </header>
      <div className="bars" role="img" aria-label={`${title} per day, last 30 days: ${total} total, at most ${max} in a day`}>
        {days.map((d) => (
          <div key={d.date} title={`${shortDate(d.date)}: ${d.count}`} style={{ height: `${(d.count / max) * 100}%` }} />
        ))}
      </div>
      {days.length > 0 ? (
        <div className="flex justify-between text-caption text-secondary">
          <span>{shortDate(days[0]!.date)}</span>
          <span>{shortDate(days[days.length - 1]!.date)}</span>
        </div>
      ) : null}
    </section>
  )
}
