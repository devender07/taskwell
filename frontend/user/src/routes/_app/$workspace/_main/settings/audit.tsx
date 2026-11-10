import { createFileRoute } from '@tanstack/react-router'
import { Pagination } from '@taskwell/shared/pagination'
import { Tabs } from '@taskwell/shared/tabs'
import { fullTime } from '@taskwell/shared/time'
import { ApiError, auditPath, type AuditCategory, type AuditPage } from '@/api'
import { loaderApi } from '@/realtime'
import { auditAction, auditDetail } from '@/components/settings/audit'

const CATEGORIES: { value: AuditCategory | 'all'; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'members', label: 'Members' },
  { value: 'roles', label: 'Roles' },
  { value: 'projects', label: 'Projects' },
  { value: 'workspace', label: 'Workspace' },
  { value: 'integrations', label: 'Integrations' },
  { value: 'trash', label: 'Trash' },
  { value: 'billing', label: 'Billing' },
]

type Search = { category?: AuditCategory; page?: number }

export const Route = createFileRoute('/_app/$workspace/_main/settings/audit')({
  validateSearch: (search): Search => ({
    category: CATEGORIES.some((c) => c.value !== 'all' && c.value === search.category) ? (search.category as AuditCategory) : undefined,
    page: Number.isInteger(search.page) && (search.page as number) > 1 ? (search.page as number) : undefined,
  }),
  loaderDeps: ({ search }) => search,
  loader: (ctx) =>
    loaderApi<AuditPage>(ctx, auditPath(ctx.params.workspace, ctx.deps.category, ctx.deps.page)).catch((e) => {
      if (e instanceof ApiError && e.status === 403) return null
      throw e
    }),
  component: AuditSettings,
})

function AuditSettings() {
  const page = Route.useLoaderData()
  const { category, page: n = 1 } = Route.useSearch()
  const navigate = Route.useNavigate()
  return (
    <main className="grid gap-6 w-full px-8 py-10" style={{ alignContent: 'start' }}>
      <header className="page-top grid gap-2">
        <h1 className="text-h2">Audit log</h1>
        <p className="text-body-sm text-secondary">Changes to members, roles, projects and integrations. Entries can't be edited or deleted.</p>
      </header>
      {page ? (
        <>
          <div className="audit-filter">
            <Tabs aria-label="Category" items={CATEGORIES} value={category ?? 'all'} onChange={(c) => navigate({ search: c === 'all' ? {} : { category: c } })} />
          </div>
          <div className="audit-wrap bg-surface border rounded-lg">
            <table className="audit-table">
              <thead>
                <tr>
                  <th>Time</th>
                  <th>Who</th>
                  <th>Action</th>
                  <th>Target</th>
                  <th>Details</th>
                  <th>IP</th>
                </tr>
              </thead>
              <tbody>
                {page.entries.map((e) => (
                  <tr key={e.id}>
                    <td className="text-secondary">{fullTime(e.at)}</td>
                    <td title={e.actor?.email}>{e.actor?.name || e.actor?.email || 'Deleted user'}</td>
                    <td>{auditAction(e.action, e.detail)}</td>
                    <td className="text-secondary">{e.target}</td>
                    <td className="text-secondary">{auditDetail(e.action, e.detail)}</td>
                    <td className="text-secondary">{e.ip}</td>
                  </tr>
                ))}
                {page.entries.length === 0 ? (
                  <tr>
                    <td colSpan={6} className="text-secondary text-center">Nothing recorded yet.</td>
                  </tr>
                ) : null}
              </tbody>
            </table>
          </div>
          <Pagination page={n} hasNext={page.more} onPage={(p) => navigate({ search: { category, page: p > 1 ? p : undefined } })} />
        </>
      ) : (
        <p className="text-body-sm text-secondary">You don't have permission to see the audit log.</p>
      )}
    </main>
  )
}
