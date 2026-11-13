import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { can, statusTypesPath, type Status, type StatusType, workspacePath } from '@/api'
import { loaderApi } from '@/realtime'
import { AddType, Workflow } from '@/components/workflow/statuses'

export const Route = createFileRoute('/_app/$workspace/_main/settings/workflow')({
  loader: async (ctx) => {
    const [statuses, types] = await Promise.all([
      loaderApi<Status[]>(ctx, `${workspacePath(ctx.params.workspace)}/statuses`),
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
      <header className="page-top flex flex-wrap items-end gap-4">
        <div className="grid gap-2 flex-grow" style={{ flexBasis: 320 }}>
          <h1 className="text-h2">Workflow</h1>
          <p className="text-body-sm text-secondary">The statuses issues move through, grouped by type.</p>
        </div>
        {manage ? <AddType workspace={workspace} /> : null}
      </header>
      <Workflow workspace={workspace} statuses={statuses} types={types} manage={manage} />
      {manage ? null : <p className="text-caption text-secondary">You don't have permission to change statuses.</p>}
    </main>
  )
}
