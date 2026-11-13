import { Link, createFileRoute, useLoaderData, useNavigate } from '@tanstack/react-router'
import { useRef } from 'react'
import { Button } from '@taskwell/shared/button'
import { Tabs } from '@taskwell/shared/tabs'
import { can, roadmapPath, type RoadmapProject, requireFeature } from '@/api'
import { loaderApi } from '@/realtime'
import { Roadmap, scrollToToday } from '@/components/roadmap/roadmap'
import type { Zoom } from '@/components/roadmap/timeline'

const zooms: { value: Zoom; label: string }[] = [
  { value: 'month', label: 'Month' },
  { value: 'quarter', label: 'Quarter' },
  { value: 'year', label: 'Year' },
]

export const Route = createFileRoute('/_app/$workspace/_main/roadmap')({
  beforeLoad: requireFeature('roadmap'),
  // quarter, the default, stays out of the URL.
  validateSearch: (search): { zoom?: 'month' | 'year' } => (search.zoom === 'month' || search.zoom === 'year' ? { zoom: search.zoom } : {}),
  loader: (ctx) => loaderApi<RoadmapProject[]>(ctx, roadmapPath(ctx.params.workspace)),
  component: RoadmapPage,
})

function RoadmapPage() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const projects = Route.useLoaderData()
  const zoom = Route.useSearch({ select: (s) => s.zoom ?? 'quarter' })
  const navigate = useNavigate({ from: Route.fullPath })
  const scrollRef = useRef<HTMLDivElement>(null)

  return (
    <main className="grid gap-6 w-full px-6 py-10" style={{ alignContent: 'start', minWidth: 0 }}>
      <header className="page-top flex flex-wrap items-center justify-between gap-4">
        <h1 className="text-h2">Roadmap</h1>
        {projects.length > 0 ? (
          <div className="flex flex-wrap items-center gap-3">
            <Button variant="ghost" onClick={() => scrollRef.current && scrollToToday(scrollRef.current)}>
              Today
            </Button>
            <Tabs aria-label="Zoom" items={zooms} value={zoom} onChange={(v) => navigate({ search: v === 'quarter' ? {} : { zoom: v }, resetScroll: false })} />
          </div>
        ) : null}
      </header>
      {projects.length === 0 ? (
        <section className="grid gap-4 p-6 bg-surface border rounded-lg justify-items-center text-center">
          <p className="text-body-sm text-secondary">No projects yet.</p>
          <Link to="/$workspace/projects" params={{ workspace: workspace.slug }} className="button button-primary">
            Go to projects
          </Link>
        </section>
      ) : (
        <Roadmap workspace={workspace} projects={projects} zoom={zoom} manage={can(workspace, 'projects.manage')} scrollRef={scrollRef} />
      )}
    </main>
  )
}
