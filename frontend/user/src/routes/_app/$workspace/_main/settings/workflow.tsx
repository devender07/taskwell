import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { can, type Status } from '@/api'
import { loaderApi } from '@/realtime'
import { AddStatus, StatusList } from '@/components/workflow/statuses'

export const Route = createFileRoute('/_app/$workspace/_main/settings/workflow')({
  loader: (ctx) => loaderApi<Status[]>(ctx, `/workspaces/${encodeURIComponent(ctx.params.workspace)}/statuses`),
  component: WorkflowSettings,
})

function WorkflowSettings() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const statuses = Route.useLoaderData()
  const manage = can(workspace, 'workflow.manage')
  return (
    <main className="grid gap-6 w-full px-6 py-10" style={{ maxWidth: 720, alignContent: 'start' }}>
      <header className="grid gap-2">
        <h1 className="text-h2">Workflow</h1>
        <p className="text-body-sm text-secondary">The statuses issues move through, in order.</p>
      </header>
      {manage ? <AddStatus workspace={workspace} statuses={statuses} /> : null}
      <StatusList workspace={workspace} statuses={statuses} manage={manage} />
      {manage ? null : <p className="text-caption text-secondary">You don't have permission to change statuses.</p>}
    </main>
  )
}
