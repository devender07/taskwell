import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { can, statusTypesPath, type Status, type StatusType } from '@/api'
import { loaderApi } from '@/realtime'
import { AddStatus, StatusList, TypeList } from '@/components/workflow/statuses'

export const Route = createFileRoute('/_app/$workspace/_main/settings/workflow')({
  loader: async (ctx) => {
    const [statuses, types] = await Promise.all([
      loaderApi<Status[]>(ctx, `/workspaces/${encodeURIComponent(ctx.params.workspace)}/statuses`),
      loaderApi<StatusType[]>(ctx, statusTypesPath(ctx.params.workspace)),
    ])
    return { statuses, types }
  },
  component: WorkflowSettings,
})

function WorkflowSettings() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const { statuses, types } = Route.useLoaderData()
  const manage = can(workspace, 'workflow.manage')
  return (
    <main className="grid gap-6 w-full px-8 py-10" style={{ alignContent: 'start' }}>
      <header className="grid gap-2">
        <h1 className="text-h2">Workflow</h1>
        <p className="text-body-sm text-secondary">The statuses issues move through, in order, and the types they can have.</p>
      </header>
      {manage ? <AddStatus workspace={workspace} types={types} /> : null}
      <StatusList workspace={workspace} statuses={statuses} types={types} manage={manage} />
      <TypeList workspace={workspace} types={types} manage={manage} />
      {manage ? null : <p className="text-caption text-secondary">You don't have permission to change statuses.</p>}
    </main>
  )
}
