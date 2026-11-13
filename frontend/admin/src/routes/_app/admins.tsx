import { createFileRoute } from '@tanstack/react-router'
import { relativeTime } from '@taskwell/shared/time'
import { formatDate, type AdminAccount } from '@/api'
import { AddAdminButton } from '@/components/admins/add-admin'
import { RemoveAdminButton } from '@/components/admins/remove-admin'
import { loaderApi } from '@/realtime'

export const Route = createFileRoute('/_app/admins')({
  loader: (ctx) => loaderApi<{ admins: AdminAccount[] }>(ctx, '/admins'),
  component: Admins,
})

function Admins() {
  const { admins } = Route.useLoaderData()
  const { admin: me } = Route.useRouteContext()
  return (
    <main className="grid gap-6 w-full px-6 py-10">
      <header className="flex items-center justify-between gap-4">
        <div className="grid gap-2">
          <h1 className="text-h2">Admins</h1>
          <p className="text-body-sm text-secondary">People who can sign in to this admin app. Every admin can add and remove others.</p>
        </div>
        <AddAdminButton />
      </header>
      <div className="table-wrap bg-surface border rounded-lg">
        <table className="table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Email</th>
              <th>Added</th>
              <th>Last active</th>
              <th>Two-step sign-in</th>
              <th aria-label="Actions" />
            </tr>
          </thead>
          <tbody>
            {admins.map((a) => (
              <tr key={a.id}>
                <td className="text-primary font-510">
                  {a.name}
                  {a.id === me.id ? <span className="text-secondary font-400"> (you)</span> : null}
                </td>
                <td className="text-secondary">{a.email}</td>
                <td className="text-secondary">{formatDate(a.created_at)}</td>
                <td className="text-secondary">{a.last_seen_at ? relativeTime(a.last_seen_at) : 'Never'}</td>
                <td>{a.mfa ? <span className="chip">On</span> : <span className="chip chip-danger">Not set up</span>}</td>
                <td className="text-right">{a.id === me.id ? null : <RemoveAdminButton admin={a} />}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </main>
  )
}
