import { createFileRoute } from '@tanstack/react-router'
import type { Defaults as Data } from '@/api'
import { loaderApi } from '@/realtime'
import { Labels } from '@/components/defaults/labels'
import { Roles } from '@/components/defaults/roles'
import { Templates } from '@/components/defaults/templates'
import { Workflow } from '@/components/defaults/workflow'

export const Route = createFileRoute('/_app/defaults')({
  loader: (ctx) => loaderApi<Data>(ctx, '/defaults'),
  component: Defaults,
})

function Defaults() {
  const d = Route.useLoaderData()
  return (
    <main className="grid gap-10 w-full px-6 py-10">
      <header className="grid gap-2">
        <h1 className="text-h2">Defaults</h1>
        <p className="text-body-sm text-secondary">
          What a new workspace starts with. It copies these once, when it's created; changes here never touch existing workspaces.
        </p>
      </header>
      <Roles roles={d.roles} catalog={d.permissions} />
      <Workflow types={d.status_types} statuses={d.statuses} />
      <Labels labels={d.labels} />
      <Templates templates={d.templates} statuses={d.statuses} types={d.status_types} labels={d.labels} />
    </main>
  )
}
