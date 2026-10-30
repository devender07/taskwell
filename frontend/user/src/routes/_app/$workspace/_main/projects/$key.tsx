import { Link, createFileRoute, notFound, redirect, useLoaderData } from '@tanstack/react-router'
import { useState } from 'react'
import { Button } from '@tether/shared/button'
import { Dialog } from '@tether/shared/dialog'
import { ApiError, api, can, type Issue, type Label, type Member, type Project, type Status } from '@/api'
import { CreateIssueForm } from '@/components/issues/issue-form'
import { IssueView } from '@/components/issues/issue-view'
import { validateViewSearch } from '@/components/issues/view-search'
import { ArchiveProject, ProjectForm } from '@/components/projects/project-form'
import '@/components/issues/issues.css'
import '@/components/projects/projects.css'

export const Route = createFileRoute('/_app/$workspace/_main/projects/$key')({
  validateSearch: validateViewSearch,
  loader: async ({ params }) => {
    const path = `/workspaces/${encodeURIComponent(params.workspace)}`
    const projectPath = `${path}/projects/${encodeURIComponent(params.key)}`
    const [project, issues, statuses, labels, { members }] = await Promise.all([
      api<Project>(projectPath),
      api<Issue[]>(`${projectPath}/issues`),
      api<Status[]>(`${path}/statuses`),
      api<Label[]>(`${path}/labels`),
      api<{ members: Member[] }>(`${path}/members`),
    ]).catch((e) => {
      if (e instanceof ApiError && e.status === 404) throw notFound()
      throw e
    })
    if (project.key !== params.key) {
      throw redirect({ to: '/$workspace/projects/$key', params: { workspace: params.workspace, key: project.key }, replace: true })
    }
    return { project, issues, statuses, labels, members }
  },
  component: ProjectPage,
})

function ProjectPage() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const { project, issues, statuses, labels, members } = Route.useLoaderData()
  const { user } = Route.useRouteContext()
  const search = Route.useSearch()
  const [editing, setEditing] = useState(false)
  const [creating, setCreating] = useState(false)
  const manage = can(workspace, 'projects.manage')
  const create = can(workspace, 'issues.create') && !project.archived

  return (
    <main className="grid gap-6 w-full px-6 py-10" style={{ alignContent: 'start' }}>
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
      <section className="grid gap-4" aria-labelledby="issues-heading">
        <div className="flex items-center justify-between gap-4">
          <h2 id="issues-heading" className="text-label">Issues</h2>
          {create && issues.length > 0 ? <Button onClick={() => setCreating(true)}>New issue</Button> : null}
        </div>
        {issues.length === 0 ? (
          <div className="grid gap-4 p-6 bg-surface border rounded-lg justify-items-center text-center">
            <p className="text-body-sm text-secondary">No issues yet.</p>
            {create ? <Button onClick={() => setCreating(true)}>New issue</Button> : null}
          </div>
        ) : (
          <IssueView workspace={workspace} issues={issues} statuses={statuses} labels={labels} members={members} userId={user.id} search={search} />
        )}
      </section>
      {create ? (
        <Dialog open={creating} onClose={() => setCreating(false)} title="New issue">
          <CreateIssueForm workspace={workspace} project={project} statuses={statuses} labels={labels} members={members} />
        </Dialog>
      ) : null}
      {manage ? (
        <Dialog open={editing} onClose={() => setEditing(false)} title="Edit project">
          <ProjectForm workspace={workspace} project={project} onDone={() => setEditing(false)} />
        </Dialog>
      ) : null}
    </main>
  )
}
