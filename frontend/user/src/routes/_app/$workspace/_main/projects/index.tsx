import { Link, createFileRoute, useLoaderData, useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Dialog } from '@taskwell/shared/dialog'
import { Input } from '@taskwell/shared/input'
import { LockIcon } from '@taskwell/shared/icons'
import { Tabs } from '@taskwell/shared/tabs'
import { can, type Project, workspacePath } from '@/api'
import { loaderApi } from '@/realtime'
import { ProjectForm } from '@/components/projects/project-form'
import { HealthPill } from '@/components/projects/updates'
import { ImportButton } from '@/components/projects/import'
import '@/components/projects/projects.css'

type View = 'active' | 'archived'

export const Route = createFileRoute('/_app/$workspace/_main/projects/')({
  // q filters the loaded list by name, key or description.
  validateSearch: (search): { archived?: true; q?: string } => ({
    ...(search.archived === true && { archived: true as const }),
    ...(typeof search.q === 'string' && search.q.trim() && { q: search.q.trim() }),
  }),
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
  const { archived, q = '' } = Route.useSearch()
  const needle = q.toLowerCase()
  const shown = needle ? projects.filter((p) => `${p.key} ${p.name} ${p.description ?? ''}`.toLowerCase().includes(needle)) : projects
  const navigate = useNavigate({ from: Route.fullPath })
  const [creating, setCreating] = useState(false)
  const manage = can(workspace, 'projects.manage')

  return (
    <main className="grid gap-6 w-full px-6 py-10" style={{ alignContent: 'start' }}>
      <header className="page-top flex items-end justify-between gap-4">
        <h1 className="text-h2">Projects</h1>
        <div className="flex flex-wrap items-center gap-3">
          {can(workspace, 'issues.create') && !archived && projects.length ? <ImportButton workspace={workspace} projects={projects} /> : null}
          {manage ? <Button onClick={() => setCreating(true)}>Create project</Button> : null}
        </div>
      </header>
      <div className="flex flex-wrap items-center gap-3">
        <Tabs
          aria-label="Projects shown"
          className="w-fit"
          items={views}
          value={archived ? 'archived' : 'active'}
          onChange={(v) => navigate({ search: (prev) => ({ q: prev.q, ...(v === 'archived' && { archived: true as const }) }) })}
        />
        <div style={{ width: 260 }}>
          <Input
            type="search"
            aria-label="Search projects"
            placeholder="Search projects"
            defaultValue={q}
            onDebouncedChange={(v) => navigate({ search: (prev) => ({ ...prev, q: v.trim() || undefined }), replace: true })}
          />
        </div>
      </div>
      {projects.length > 0 && shown.length === 0 ? (
        <p className="text-body-sm text-secondary p-6 bg-surface border rounded-lg text-center">No projects match your search.</p>
      ) : projects.length === 0 ? (
        <section className="grid gap-4 p-6 bg-surface border rounded-lg justify-items-center text-center">
          <p className="text-body-sm text-secondary">{archived ? 'No archived projects.' : 'No projects yet.'}</p>
          {manage && !archived ? <Button onClick={() => setCreating(true)}>Create project</Button> : null}
        </section>
      ) : (
        <section className="bg-surface border rounded-lg overflow-hidden" aria-label="Projects">
          <ul className="grid m-0 p-0" style={{ listStyle: 'none' }}>
            {shown.map((p) => (
              <li key={p.id}>
                <Link to="/$workspace/projects/$key" params={{ workspace: workspace.slug, key: p.key }} className="project-row">
                  <span className="project-key flex-noshrink" style={{ width: 56 }}>{p.key}</span>
                  <div className="grid flex-grow" style={{ minWidth: 0 }}>
                    <span className="flex items-center gap-2" style={{ minWidth: 0 }}>
                      {p.private ? <LockIcon size={14} className="text-secondary flex-noshrink" role="img" aria-hidden={false} aria-label="Private" /> : null}
                      <span className="text-label text-primary ellipsis">{p.name}</span>
                      <HealthPill health={p.health} />
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
