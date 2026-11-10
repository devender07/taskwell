import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { can, templatesPath, type Label, type Project, type Status, type Template, workspacePath, requireFeature } from '@/api'
import { loaderApi } from '@/realtime'
import { Templates } from '@/components/templates/templates'

export const Route = createFileRoute('/_app/$workspace/_main/settings/templates')({
  beforeLoad: requireFeature('templates'),
  loader: async (ctx) => {
    const path = workspacePath(ctx.params.workspace)
    const [templates, projects, statuses, labels] = await Promise.all([
      loaderApi<Template[]>(ctx, templatesPath(ctx.params.workspace)),
      loaderApi<Project[]>(ctx, `${path}/projects`),
      loaderApi<Status[]>(ctx, `${path}/statuses`),
      loaderApi<Label[]>(ctx, `${path}/labels`),
    ])
    return { templates, projects, statuses, labels }
  },
  component: TemplatesSettings,
})

function TemplatesSettings() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const { templates, projects, statuses, labels } = Route.useLoaderData()
  const manage = can(workspace, 'workflow.manage')
  return (
    <main className="grid gap-6 w-full px-8 py-10" style={{ alignContent: 'start', minWidth: 0 }}>
      <header className="page-top grid gap-2">
        <h1 className="text-h2">Templates</h1>
        <p className="text-body-sm text-secondary">Prefill new issues, or create one on a schedule.</p>
      </header>
      <Templates workspace={workspace} templates={templates} projects={projects} statuses={statuses} labels={labels} manage={manage} />
      {manage ? null : <p className="text-caption text-secondary">You don't have permission to change templates.</p>}
    </main>
  )
}
