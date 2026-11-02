import { Link, createFileRoute, useLoaderData, useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { Button } from '@tether/shared/button'
import { Dialog } from '@tether/shared/dialog'
import { LockIcon } from '@tether/shared/icons'
import { Tabs } from '@tether/shared/tabs'
import { can, type Project, workspacePath } from '@/api'
import { loaderApi } from '@/realtime'
import { ProjectForm } from '@/components/projects/project-form'
import '@/components/projects/projects.css'

type View = 'active' | 'archived'

export const Route = createFileRoute('/_app/$workspace/_main/projects/')({
  validateSearch: (search): { archived?: true } => (search.archived === true ? { archived: true } : {}),
  loaderDeps: ({ search }) => ({ archived: search.archived }),
  loader: (ctx) =>
    loaderApi<Project[]>(ctx, `${workspacePath(ctx.params.workspace)}/projects${ctx.deps.archived ? '?archived=true' : ''}`),
  component: Projects,
})

const views: { value: View; label: string }[] = [
  { value: 'active', label: 'Active' },
  { value: 'archived', label: 'Archived' },
]

function Projects() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const projects = Route.useLoaderData()
  const { archived } = Route.useSearch()
  const navigate = useNavigate({ from: Route.fullPath })
  const [creating, setCreating] = useState(false)
  const manage = can(workspace, 'projects.manage')

  return (
    <main className="grid gap-6 w-full px-6 py-10" style={{ alignContent: 'start' }}>
      <header className="flex items-end justify-between gap-4">
        <h1 className="text-h2">Projects</h1>
        {manage ? <Button onClick={() => setCreating(true)}>Create project</Button> : null}
      </header>
      <Tabs
        aria-label="Projects shown"
        className="w-fit"
        items={views}
        value={archived ? 'archived' : 'active'}
        onChange={(v) => navigate({ search: v === 'archived' ? { archived: true } : {} })}
      />
      {projects.length === 0 ? (
        <section className="grid gap-4 p-6 bg-surface border rounded-lg justify-items-center text-center">
          <p className="text-body-sm text-secondary">{archived ? 'No archived projects.' : 'No projects yet.'}</p>
          {manage && !archived ? <Button onClick={() => setCreating(true)}>Create project</Button> : null}
        </section>
      ) : (
        <section className="bg-surface border rounded-lg overflow-hidden" aria-label="Projects">
          <ul className="grid m-0 p-0" style={{ listStyle: 'none' }}>
            {projects.map((p) => (
              <li key={p.id}>
                <Link to="/$workspace/projects/$key" params={{ workspace: workspace.slug, key: p.key }} className="project-row">
                  <span className="project-key flex-noshrink" style={{ width: 56 }}>{p.key}</span>
                  <div className="grid flex-grow" style={{ minWidth: 0 }}>
                    <span className="flex items-center gap-2" style={{ minWidth: 0 }}>
                      {p.private ? <LockIcon size={14} className="text-secondary flex-noshrink" role="img" aria-hidden={false} aria-label="Private" /> : null}
                      <span className="text-label text-primary ellipsis">{p.name}</span>
                    </span>
                    {p.description ? <span className="text-caption text-secondary ellipsis">{p.description}</span> : null}
                  </div>
                </Link>
              </li>
            ))}
          </ul>
        </section>
      )}
      {manage ? (
        <Dialog open={creating} onClose={() => setCreating(false)} title="Create project">
          <ProjectForm workspace={workspace} onDone={() => setCreating(false)} />
        </Dialog>
      ) : null}
    </main>
  )
}
