import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { api, can, type Label } from '@/api'
import { AddLabel, LabelList } from '@/components/workflow/labels'

export const Route = createFileRoute('/_app/$workspace/_main/settings/labels')({
  loader: ({ params }) => api<Label[]>(`/workspaces/${encodeURIComponent(params.workspace)}/labels`),
  component: LabelsSettings,
})

function LabelsSettings() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const labels = Route.useLoaderData()
  const manage = can(workspace, 'workflow.manage')
  return (
    <main className="grid gap-6 w-full px-6 py-10" style={{ maxWidth: 720, alignContent: 'start' }}>
      <header className="grid gap-2">
        <h1 className="text-h2">Labels</h1>
        <p className="text-body-sm text-secondary">Tags for grouping issues across projects.</p>
      </header>
      {manage ? <AddLabel workspace={workspace} /> : null}
      <LabelList workspace={workspace} labels={labels} manage={manage} />
      {manage ? null : <p className="text-caption text-secondary">You don't have permission to change labels.</p>}
    </main>
  )
}
