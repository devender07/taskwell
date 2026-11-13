import { createFileRoute } from '@tanstack/react-router'
import { listQuery, validateListSearch, type AuditEntry } from '@/api'
import { Pager } from '@/components/app/list-controls'
import { loaderApi } from '@/realtime'

export const Route = createFileRoute('/_app/audit')({
  validateSearch: (search): { page?: number } => {
    const { page } = validateListSearch(search)
    return page ? { page } : {}
  },
  loaderDeps: ({ search }) => search,
  loader: (ctx) => loaderApi<{ entries: AuditEntry[]; total: number }>(ctx, `/audit?${listQuery(ctx.deps)}`),
  component: Audit,
})

const ACTIONS: Record<string, string> = {
  'config.update': 'Changed setting',
  'user.suspend': 'Suspended user',
  'user.unsuspend': 'Unsuspended user',
  'admin.promote': 'Made platform admin',
  'admin.mfa_reset': 'Reset two-step sign-in',
  'admin.create': 'Added admin',
  'admin.remove': 'Removed admin',
  signin: 'Signed in',
  'signin.failed': 'Failed sign-in',
  'signin.mfa_failed': 'Wrong sign-in code',
  'mfa.enrolled': 'Set up two-step sign-in',
  'mfa.recovery_used': 'Used a recovery code',
  'defaults.update': 'Changed workspace defaults',
  'dev.reset': 'Deleted all user data',
}

const formatTime = (iso: string) =>
  new Date(iso).toLocaleString(undefined, { month: 'short', day: 'numeric', year: 'numeric', hour: 'numeric', minute: '2-digit' })

function Audit() {
  const { entries, total } = Route.useLoaderData()
  const search = Route.useSearch()
  return (
    <main className="grid gap-6 w-full px-6 py-10">
      <header className="grid gap-2">
        <h1 className="text-h2">Audit log</h1>
        <p className="text-body-sm text-secondary">Admin sign-ins, setting changes, suspensions and admin accounts added or removed. Entries can't be edited or deleted.</p>
      </header>
      <div className="table-wrap bg-surface border rounded-lg">
        <table className="table">
          <thead>
            <tr>
              <th>Time</th>
              <th>Who</th>
              <th>Action</th>
              <th>Target</th>
              <th>IP</th>
            </tr>
          </thead>
          <tbody>
            {entries.map((e) => (
              <tr key={e.id}>
                <td className="text-secondary">{formatTime(e.at)}</td>
                <td className="text-primary">{e.actor_email || (e.ip === 'cli' ? 'Command line' : 'Unknown')}</td>
                <td className={e.action.endsWith('failed') ? 'text-accent' : undefined}>{ACTIONS[e.action] ?? e.action}</td>
                <td className="text-secondary">{e.target}</td>
                <td className="text-secondary">{e.ip}</td>
              </tr>
            ))}
            {entries.length === 0 ? (
              <tr>
                <td colSpan={5} className="text-secondary text-center">Nothing recorded yet.</td>
              </tr>
            ) : null}
          </tbody>
        </table>
      </div>
      <Pager to="/audit" search={search} total={total} />
    </main>
  )
}
