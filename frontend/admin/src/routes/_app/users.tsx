import { createFileRoute } from '@tanstack/react-router'
import { relativeTime } from '@taskwell/shared/time'
import { formatDate, listQuery, PLANS, validateListSearch, type UserRow } from '@/api'
import { Pager, SearchForm } from '@/components/app/list-controls'
import { PlanButton } from '@/components/users/plan-button'
import { SuspendButton } from '@/components/users/suspend-button'
import { loaderApi } from '@/realtime'

export const Route = createFileRoute('/_app/users')({
  validateSearch: validateListSearch,
  loaderDeps: ({ search }) => search,
  loader: (ctx) => loaderApi<{ users: UserRow[]; total: number }>(ctx, `/users?${listQuery(ctx.deps)}`),
  component: Users,
})

function Users() {
  const { users, total } = Route.useLoaderData()
  const search = Route.useSearch()
  return (
    <main className="grid gap-6 w-full px-6 py-10">
      <h1 className="text-h2">Users</h1>
      <SearchForm to="/users" search={search} placeholder="Search by name or email" />
      <div className="table-wrap bg-surface border rounded-lg">
        <table className="table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Email</th>
              <th>Plan</th>
              <th className="num">Workspaces</th>
              <th>Joined</th>
              <th>Last active</th>
              <th>Status</th>
              <th aria-label="Actions" />
            </tr>
          </thead>
          <tbody>
            {users.map((u) => (
              <tr key={u.id}>
                <td className="text-primary font-510">{u.name}</td>
                <td className="text-secondary">{u.email}</td>
                <td>
                  {PLANS[u.plan] ?? u.plan}
                  {u.comped ? <span className="text-secondary"> · comped</span> : null}
                </td>
                <td className="num">{u.workspaces}</td>
                <td className="text-secondary">{formatDate(u.created_at)}</td>
                <td className="text-secondary">{u.last_active_at ? relativeTime(u.last_active_at) : 'Never'}</td>
                <td>
                  {u.suspended ? <span className="chip chip-danger">Suspended</span> : null}
                </td>
                <td className="text-right">
                  <PlanButton user={u} />
                  <SuspendButton user={u} />
                </td>
              </tr>
            ))}
            {users.length === 0 ? (
              <tr>
                <td colSpan={8} className="text-secondary text-center">No users found.</td>
              </tr>
            ) : null}
          </tbody>
        </table>
      </div>
      <Pager to="/users" search={search} total={total} />
    </main>
  )
}
