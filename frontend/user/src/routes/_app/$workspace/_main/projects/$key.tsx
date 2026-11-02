import { Link, createFileRoute, notFound, redirect, useLoaderData } from '@tanstack/react-router'
import { BackLink } from '@tether/shared/back-link'
import { useState } from 'react'
import { Button } from '@tether/shared/button'
import { Dialog } from '@tether/shared/dialog'
import { LockIcon } from '@tether/shared/icons'
import { ApiError, can, cyclesPath, projectPeoplePath, type Cycle, type Issue, type Label, type Member, type Project, type Status, workspacePath } from '@/api'
import { loaderApi, trackPath } from '@/realtime'
import { useShortcuts } from '@/components/app/shortcuts'
import { formatDate } from '@/components/issues/fields'
import { IssueView } from '@/components/issues/issue-view'
import { validateViewSearch } from '@/components/issues/view-search'
import { ArchiveProject, ProjectForm, ProjectMembers } from '@/components/projects/project-form'
import '@/components/issues/issues.css'
import '@/components/projects/projects.css'

export const Route = createFileRoute('/_app/$workspace/_main/projects/$key')({
  validateSearch: validateViewSearch,
  loader: async (ctx) => {
    const { params } = ctx
    const path = `${workspacePath(params.workspace)}`
    const projectPath = `${path}/projects/${encodeURIComponent(params.key)}`
    trackPath(ctx, `${path}/members`)
    const [project, issues, cycles, statuses, labels, members] = await Promise.all([
      loaderApi<Project>(ctx, projectPath),
      loaderApi<Issue[]>(ctx, `${projectPath}/issues`),
      loaderApi<Cycle[]>(ctx, cyclesPath(params.workspace, params.key)),
      loaderApi<Status[]>(ctx, `${path}/statuses`),
      loaderApi<Label[]>(ctx, `${path}/labels`),
      loaderApi<Member[]>(ctx, projectPeoplePath(params.workspace, params.key)),
    ]).catch((e) => {
      if (e instanceof ApiError && e.status === 404) throw notFound()
      throw e
    })
    if (project.key !== params.key) {
      throw redirect({ to: '/$workspace/projects/$key', params: { workspace: params.workspace, key: project.key }, replace: true })
    }
    return { project, issues, cycles, statuses, labels, members }
  },
  component: ProjectPage,
})

function ProjectPage() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const { project, issues, cycles, statuses, labels, members } = Route.useLoaderData()
  const { user } = Route.useRouteContext()
  const search = Route.useSearch()
  const [editing, setEditing] = useState(false)
  const { openCreate } = useShortcuts()
  const manage = can(workspace, 'projects.manage')
  const create = can(workspace, 'issues.create') && !project.archived
  const current = cycles.find((c) => c.state === 'current')

  return (
    <main className="grid gap-6 w-full px-6 py-10" style={{ alignContent: 'start' }}>
      <header className="grid gap-2">
        <BackLink
          to="/$workspace/projects"
          params={{ workspace: workspace.slug }}
          search={project.archived ? { archived: true } : {}}
        >
          Projects
        </BackLink>
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div className="flex items-center gap-3" style={{ minWidth: 0 }}>
            <span className="project-key flex-noshrink">{project.key}</span>
            {project.private ? <LockIcon className="text-secondary flex-noshrink" /> : null}
            <h1 className="text-h2 ellipsis">{project.name}</h1>
            {project.private ? <span className="project-tag flex-noshrink">Private</span> : null}
            {project.archived ? <span className="project-tag flex-noshrink">Archived</span> : null}
          </div>
          <div className="flex flex-wrap items-center gap-3">
            {current ? (
              <Link
                to="/$workspace/projects/$key/cycles/$number"
                params={{ workspace: workspace.slug, key: project.key, number: String(current.number) }}
                className="project-tag"
                style={{ textDecoration: 'none' }}
              >
                {current.name} · ends {formatDate(current.ends_on)}
              </Link>
            ) : null}
            <Link to="/$workspace/projects/$key/cycles" params={{ workspace: workspace.slug, key: project.key }} className="button button-ghost">
              Cycles
            </Link>
            {manage ? (
              <>
                <Button variant="secondary" onClick={() => setEditing(true)}>
                  Edit
                </Button>
                {project.private ? <ProjectMembers workspace={workspace} project={project} /> : null}
                <ArchiveProject workspace={workspace} project={project} />
              </>
            ) : null}
          </div>
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
          {create && issues.length > 0 ? <Button title="New issue (C)" onClick={openCreate}>New issue</Button> : null}
        </div>
        {issues.length === 0 ? (
          <div className="grid gap-4 p-6 bg-surface border rounded-lg justify-items-center text-center">
            <p className="text-body-sm text-secondary">No issues yet.</p>
            {create ? <Button title="New issue (C)" onClick={openCreate}>New issue</Button> : null}
          </div>
        ) : (
          <IssueView workspace={workspace} issues={issues} statuses={statuses} labels={labels} members={members} userId={user.id} search={search} cycles={cycles} />
        )}
      </section>
      {manage ? (
        <Dialog open={editing} onClose={() => setEditing(false)} title="Edit project">
          <ProjectForm workspace={workspace} project={project} onDone={() => setEditing(false)} />
        </Dialog>
      ) : null}
    </main>
  )
}
