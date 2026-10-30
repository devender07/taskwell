import { Link, createFileRoute, useLoaderData } from '@tanstack/react-router'
import { api, type Permission } from '@/api'
import { RoleForm } from '@/components/roles/role-form'

export const Route = createFileRoute('/_app/$workspace/_main/settings/roles/new')({
  loader: () => api<Permission[]>('/permissions'),
  component: NewRole,
})

function NewRole() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const catalog = Route.useLoaderData()
  return (
    <main className="grid gap-6 w-full px-6 py-10" style={{ maxWidth: 720, alignContent: 'start' }}>
      <header className="grid gap-2">
        <Link to="/$workspace/settings/roles" params={{ workspace: workspace.slug }} className="text-caption text-secondary">
          ← Roles
        </Link>
        <h1 className="text-h2">Create role</h1>
        <p className="text-body-sm text-secondary">Pick what people with this role can do.</p>
      </header>
      <RoleForm key={workspace.id} workspace={workspace} catalog={catalog} />
    </main>
  )
}
