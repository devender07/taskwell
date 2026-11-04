import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { can, type Project, workspacePath } from '@/api'
import { loaderApi } from '@/realtime'
import { ImportForm } from '@/components/settings/import'

export const Route = createFileRoute('/_app/$workspace/_main/settings/import')({
  // project: the key to import into, preselected (from a project's empty state).
  validateSearch: (search): { project?: string } => (typeof search.project === 'string' ? { project: search.project } : {}),
  loader: (ctx) => loaderApi<Project[]>(ctx, `${workspacePath(ctx.params.workspace)}/projects`),
  component: ImportSettings,
})

function ImportSettings() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const projects = Route.useLoaderData()
  const { project } = Route.useSearch()
  return (
    <main className="grid gap-6 w-full px-8 py-10" style={{ alignContent: 'start' }}>
      <header className="grid gap-2">
        <h1 className="text-h2">Import</h1>
        <p className="text-body-sm text-secondary">Bring issues into a project from a CSV export or a GitHub repository. Preview first; nothing changes until you import.</p>
      </header>
      {can(workspace, 'issues.create') ? (
        <ImportForm workspace={workspace} projects={projects} project={project} />
      ) : (
        <p className="text-body-sm text-secondary">You don't have permission to create issues.</p>
      )}
    </main>
  )
}
