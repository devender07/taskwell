import { Link, createFileRoute, notFound, redirect, useLoaderData } from '@tanstack/react-router'
import { useState } from 'react'
import { Button } from '@tether/shared/button'
import { Dialog } from '@tether/shared/dialog'
import { ApiError, can, cyclesPath, type Cycle, type Project } from '@/api'
import { loaderApi, trackPath } from '@/realtime'
import { CycleForm, CycleProgress } from '@/components/cycles/cycles'
import { formatRange } from '@/components/issues/fields'
import '@/components/projects/projects.css'

export const Route = createFileRoute('/_app/$workspace/_main/projects/$key_/cycles/')({
  loader: async (ctx) => {
    const { params } = ctx
    const projectPath = `/workspaces/${encodeURIComponent(params.workspace)}/projects/${encodeURIComponent(params.key)}`
    trackPath(ctx, `${projectPath}/issues`) // progress follows status changes
    const [project, cycles] = await Promise.all([
      loaderApi<Project>(ctx, projectPath),
      loaderApi<Cycle[]>(ctx, cyclesPath(params.workspace, params.key)),
    ]).catch((e) => {
      if (e instanceof ApiError && e.status === 404) throw notFound()
      throw e
    })
    if (project.key !== params.key) {
      throw redirect({ to: '/$workspace/projects/$key/cycles', params: { workspace: params.workspace, key: project.key }, replace: true })
    }
    return { project, cycles }
  },
  component: CyclesPage,
})

const SECTIONS = [
  { state: 'current', title: 'Current' },
  { state: 'upcoming', title: 'Upcoming' },
  { state: 'past', title: 'Past' },
] as const

function CyclesPage() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const { project, cycles } = Route.useLoaderData()
  const [creating, setCreating] = useState(false)
  const manage = can(workspace, 'cycles.manage') && !project.archived

  return (
    <main className="grid gap-6 w-full px-6 py-10" style={{ alignContent: 'start' }}>
      <header className="grid gap-2">
        <Link to="/$workspace/projects/$key" params={{ workspace: workspace.slug, key: project.key }} className="text-caption text-secondary">
          ← {project.name}
        </Link>
        <div className="flex flex-wrap items-center justify-between gap-4">
          <h1 className="text-h2">Cycles</h1>
          {manage && cycles.length > 0 ? <Button onClick={() => setCreating(true)}>New cycle</Button> : null}
        </div>
      </header>
      {cycles.length === 0 ? (
        <section className="grid gap-4 p-6 bg-surface border rounded-lg justify-items-center text-center">
          <p className="text-body-sm text-secondary">No cycles yet.</p>
          {manage ? <Button onClick={() => setCreating(true)}>New cycle</Button> : null}
        </section>
      ) : (
        SECTIONS.map(({ state, title }) => {
          const list = cycles.filter((c) => c.state === state)
          if (!list.length) return null
          return (
            <section key={state} className="grid gap-3" aria-labelledby={`cycles-${state}`}>
              <h2 id={`cycles-${state}`} className="text-label">{title}</h2>
              <ul className="grid m-0 p-0 bg-surface border rounded-lg overflow-hidden" style={{ listStyle: 'none' }}>
                {list.map((c) => (
                  <li key={c.id}>
                    <Link
                      to="/$workspace/projects/$key/cycles/$number"
                      params={{ workspace: workspace.slug, key: project.key, number: String(c.number) }}
                      className="project-row"
                    >
                      <div className="grid flex-grow" style={{ minWidth: 0 }}>
                        <span className="text-label text-primary ellipsis">{c.name}</span>
                        <span className="text-caption text-secondary">{formatRange(c)}</span>
                      </div>
                      <CycleProgress cycle={c} />
                    </Link>
                  </li>
                ))}
              </ul>
            </section>
          )
        })
      )}
      {manage ? (
        <Dialog open={creating} onClose={() => setCreating(false)} title="New cycle">
          <CycleForm workspace={workspace} project={project} cycles={cycles} onDone={() => setCreating(false)} />
        </Dialog>
      ) : null}
    </main>
  )
}
