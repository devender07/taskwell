import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { BackLink } from '@tether/shared/back-link'
import type { Permission } from '@/api'
import { loaderApi } from '@/realtime'
import { RoleForm } from '@/components/roles/role-form'

export const Route = createFileRoute('/_app/$workspace/_main/settings/roles/new')({
  loader: (ctx) => loaderApi<Permission[]>(ctx, '/permissions'),
  component: NewRole,
})

function NewRole() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const catalog = Route.useLoaderData()
  return (
    <main className="grid gap-6 w-full px-8 py-10" style={{ alignContent: 'start' }}>
      <header className="grid gap-2">
        <BackLink to="/$workspace/settings/roles" params={{ workspace: workspace.slug }}>
          Roles
        </BackLink>
        <h1 className="text-h2">Create role</h1>
        <p className="text-body-sm text-secondary">Pick what people with this role can do.</p>
      </header>
      <RoleForm key={workspace.id} workspace={workspace} catalog={catalog} />
    </main>
  )
}
