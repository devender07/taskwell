import { Link, createFileRoute } from '@tanstack/react-router'
import { ChevronDownIcon, ChevronUpIcon } from '@taskwell/shared/icons'
import { ApiError, teamAnalyticsPath, type TeamMember, requireFeature } from '@/api'
import { loaderApi } from '@/realtime'
import { days, sortTeam, validateTeamSearch, type TeamSort } from '@/components/analytics/analytics'
import { useRefetchEvery } from '@/components/analytics/member-analytics'

const COLUMNS: { key: TeamSort; label: string }[] = [
  { key: 'name', label: 'Member' },
  { key: 'open', label: 'Open' },
  { key: 'overdue', label: 'Overdue' },
  { key: 'completed_30d', label: 'Completed (30 days)' },
  { key: 'cycle_time_days', label: 'Median cycle time' },
]

export const Route = createFileRoute('/_app/$workspace/_main/team')({
  beforeLoad: requireFeature('team'),
  validateSearch: validateTeamSearch,
  loader: (ctx) =>
    loaderApi<{ members: TeamMember[] }>(ctx, teamAnalyticsPath(ctx.params.workspace)).catch((e) => {
      if (e instanceof ApiError && e.status === 403) return null
      throw e
    }),
  component: TeamPage,
})

function TeamPage() {
  const { workspace } = Route.useParams()
  const team = Route.useLoaderData()
  const search = Route.useSearch()
  const sort = search.sort ?? 'name'
  useRefetchEvery(Route.id)

  return (
    <main className="grid gap-6 w-full px-6 py-10" style={{ alignContent: 'start' }}>
      <header className="page-top grid gap-1">
        <h1 className="text-h2">Team</h1>
        <p className="text-body-sm text-secondary m-0">Open work and what each member finished recently.</p>
      </header>
      {!team ? (
        <p className="text-body-sm text-secondary m-0">You don't have permission to see team analytics.</p>
      ) : team.members.length === 0 ? (
        <p className="text-body-sm text-secondary m-0">No members yet.</p>
      ) : (
        <div className="bg-surface border rounded-lg overflow-x-auto">
          <table className="analytics-table text-body-sm">
            <thead>
              <tr>
                {COLUMNS.map((c) => {
                  const active = c.key === sort
                  const Arrow = search.desc ? ChevronDownIcon : ChevronUpIcon
                  return (
                    <th key={c.key} scope="col" aria-sort={active ? (search.desc ? 'descending' : 'ascending') : undefined}>
                      <Link
                        from={Route.fullPath}
                        search={() => ({ ...(c.key === 'name' ? {} : { sort: c.key }), ...(active && !search.desc ? { desc: true as const } : {}) })}
                        replace
                        className="analytics-sort"
                      >
                        {c.label}
                        {active ? <Arrow size={14} strokeWidth={2} /> : null}
                      </Link>
                    </th>
                  )
                })}
              </tr>
            </thead>
            <tbody>
              {sortTeam(team.members, { sort, desc: search.desc }).map((m) => (
                <tr key={m.id}>
                  <td>
                    <Link to="/$workspace/members/$id" params={{ workspace, id: m.id }} className="member-link text-label">
                      {m.name}
                    </Link>
                  </td>
                  <td>{m.open}</td>
                  <td className={m.overdue ? 'text-accent' : undefined}>{m.overdue}</td>
                  <td>{m.completed_30d}</td>
                  <td className={m.cycle_time_days === null ? 'text-secondary' : undefined}>{m.cycle_time_days === null ? '—' : days(m.cycle_time_days)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </main>
  )
}
