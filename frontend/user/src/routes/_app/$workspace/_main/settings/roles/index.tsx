import { Link, createFileRoute, useLoaderData } from '@tanstack/react-router'
import { can, type Role, workspacePath } from '@/api'
import { loaderApi } from '@/realtime'
import '@/components/roles/roles.css'

export const Route = createFileRoute('/_app/$workspace/_main/settings/roles/')({
  loader: (ctx) => loaderApi<Role[]>(ctx, `${workspacePath(ctx.params.workspace)}/roles`),
  component: RolesSettings,
})

function RolesSettings() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const roles = Route.useLoaderData()
  return (
    <main className="grid gap-6 w-full px-8 py-10" style={{ alignContent: 'start' }}>
      <header className="flex items-end justify-between gap-4">
        <div className="grid gap-2">
          <h1 className="text-h2">Roles</h1>
          <p className="text-body-sm text-secondary">What people in {workspace.name} can do.</p>
        </div>
        {can(workspace, 'roles.manage') ? (
          <Link to="/$workspace/settings/roles/new" params={{ workspace: workspace.slug }} className="button button-primary">
            Create role
          </Link>
        ) : null}
      </header>
      <section className="bg-surface border rounded-lg overflow-hidden" aria-label="Roles">
        <ul className="grid m-0 p-0" style={{ listStyle: 'none' }}>
          {roles.map((r) => (
            <li key={r.id}>
              <Link to="/$workspace/settings/roles/$roleId" params={{ workspace: workspace.slug, roleId: r.id }} className="role-row">
                <div className="grid flex-grow" style={{ minWidth: 0 }}>
                  <span className="flex items-center gap-2 text-label text-primary">
                    <span className="ellipsis">{r.name}</span>
                    {r.builtin ? <span className="role-tag flex-noshrink">Built-in</span> : null}
                  </span>
                  <span className="text-caption text-secondary">{summary(r)}</span>
                </div>
                <span className="text-caption text-secondary flex-noshrink">
                  {r.member_count} {r.member_count === 1 ? 'member' : 'members'}
                </span>
              </Link>
            </li>
          ))}
        </ul>
      </section>
    </main>
  )
}

function summary(role: Role) {
  if (role.builtin === 'admin') return 'All permissions'
  const n = role.permissions.length
  return n === 0 ? 'No permissions' : `${n} ${n === 1 ? 'permission' : 'permissions'}`
}
