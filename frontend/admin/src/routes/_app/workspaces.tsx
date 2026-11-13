import { createFileRoute } from '@tanstack/react-router'
import { formatDate, listQuery, PLANS, validateListSearch, type WorkspaceRow } from '@/api'
import { Pager, SearchForm } from '@/components/app/list-controls'
import { loaderApi } from '@/realtime'

export const Route = createFileRoute('/_app/workspaces')({
  validateSearch: validateListSearch,
  loaderDeps: ({ search }) => search,
  loader: (ctx) => loaderApi<{ workspaces: WorkspaceRow[]; total: number }>(ctx, `/workspaces?${listQuery(ctx.deps)}`),
  component: Workspaces,
})

function Workspaces() {
  const { workspaces, total } = Route.useLoaderData()
  const search = Route.useSearch()
  return (
    <main className="grid gap-6 w-full px-6 py-10">
      <h1 className="text-h2">Workspaces</h1>
      <SearchForm to="/workspaces" search={search} placeholder="Search by name or URL" />
      <div className="table-wrap bg-surface border rounded-lg">
        <table className="table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Slug</th>
              <th>Plan</th>
              <th className="num">Members</th>
              <th className="num">Projects</th>
              <th className="num">Issues</th>
              <th>Created</th>
            </tr>
          </thead>
          <tbody>
            {workspaces.map((w) => (
              <tr key={w.id}>
                <td className="text-primary font-510">{w.name}</td>
                <td className="text-secondary">{w.slug}</td>
                <td>
                  {PLANS[w.plan] ?? w.plan}
                  {w.comped ? <span className="text-secondary"> · comped</span> : null}
                </td>
                <td className="num">{w.members}</td>
                <td className="num">{w.projects}</td>
                <td className="num">{w.issues}</td>
                <td className="text-secondary">{formatDate(w.created_at)}</td>
              </tr>
            ))}
            {workspaces.length === 0 ? (
              <tr>
                <td colSpan={7} className="text-secondary text-center">No workspaces found.</td>
              </tr>
            ) : null}
          </tbody>
        </table>
      </div>
      <Pager to="/workspaces" search={search} total={total} />
    </main>
  )
}
