import { Link, createFileRoute, notFound, useLoaderData } from '@tanstack/react-router'
import { api, can, type Permission, type Role } from '@/api'
import { DeleteRole, RoleForm } from '@/components/roles/role-form'

export const Route = createFileRoute('/_app/$workspace/_main/settings/roles/$roleId')({
  loader: async ({ params }) => {
    const [catalog, roles] = await Promise.all([
      api<Permission[]>('/permissions'),
      api<Role[]>(`/workspaces/${encodeURIComponent(params.workspace)}/roles`),
    ])
    const role = roles.find((r) => r.id === params.roleId)
    if (!role) throw notFound()
    return { catalog, role }
  },
  component: EditRole,
})

function EditRole() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const { catalog, role } = Route.useLoaderData()
  return (
    <main className="grid gap-6 w-full px-6 py-10" style={{ maxWidth: 720, alignContent: 'start' }}>
      <header className="grid gap-2">
        <Link to="/$workspace/settings/roles" params={{ workspace: workspace.slug }} className="text-caption text-secondary">
          ← Roles
        </Link>
        <h1 className="text-h2">{role.name}</h1>
        <p className="text-body-sm text-secondary">
          {role.member_count} {role.member_count === 1 ? 'member' : 'members'}
        </p>
      </header>
      <RoleForm key={role.id} workspace={workspace} catalog={catalog} role={role} />
      {can(workspace, 'roles.manage') && !role.builtin ? <DeleteRole workspace={workspace} role={role} /> : null}
    </main>
  )
}
