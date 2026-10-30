import { Link, createFileRoute, notFound, useLoaderData } from '@tanstack/react-router'
import { useState } from 'react'
import { Button } from '@tether/shared/button'
import { Dialog } from '@tether/shared/dialog'
import { ApiError, api, can, type Project } from '@/api'
import { ArchiveProject, ProjectForm } from '@/components/projects/project-form'
import '@/components/projects/projects.css'

export const Route = createFileRoute('/_app/$workspace/_main/projects/$key')({
  loader: async ({ params }) => {
    try {
      return await api<Project>(`/workspaces/${encodeURIComponent(params.workspace)}/projects/${encodeURIComponent(params.key)}`)
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) throw notFound()
      throw e
    }
  },
  component: ProjectPage,
})

function ProjectPage() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const project = Route.useLoaderData()
  const [editing, setEditing] = useState(false)
  const manage = can(workspace, 'projects.manage')

  return (
    <main className="grid gap-6 w-full px-6 py-10" style={{ maxWidth: 720, alignContent: 'start' }}>
      <header className="grid gap-2">
        <Link
          to="/$workspace/projects"
          params={{ workspace: workspace.slug }}
          search={project.archived ? { archived: true } : {}}
          className="text-caption text-secondary"
        >
          ← Projects
        </Link>
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div className="flex items-center gap-3" style={{ minWidth: 0 }}>
            <span className="project-key flex-noshrink">{project.key}</span>
            <h1 className="text-h2 ellipsis">{project.name}</h1>
            {project.archived ? <span className="project-tag flex-noshrink">Archived</span> : null}
          </div>
          {manage ? (
            <div className="flex items-start gap-3">
              <Button variant="secondary" onClick={() => setEditing(true)}>
                Edit
              </Button>
              <ArchiveProject workspace={workspace} project={project} />
            </div>
          ) : null}
        </div>
      </header>
      {project.description ? (
        <p className="text-body-sm m-0" style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{project.description}</p>
      ) : (
        <p className="text-body-sm text-secondary m-0">No description.</p>
      )}
      <section className="p-6 bg-surface border rounded-lg">
        <p className="text-body-sm text-secondary m-0">Issues will show up here.</p>
      </section>
      {manage ? (
        <Dialog open={editing} onClose={() => setEditing(false)} title="Edit project">
          <ProjectForm workspace={workspace} project={project} onDone={() => setEditing(false)} />
        </Dialog>
      ) : null}
    </main>
  )
}
