import { Link, createFileRoute, notFound, redirect, useLoaderData, useNavigate } from '@tanstack/react-router'
import { BackLink } from '@tether/shared/back-link'
import { useState } from 'react'
import { Button } from '@tether/shared/button'
import { Dialog } from '@tether/shared/dialog'
import { LockIcon } from '@tether/shared/icons'
import { ApiError, can, cyclesPath, type Cycle, type Label, type Project, type Status, workspacePath } from '@/api'
import { loaderApi } from '@/realtime'
import { useShortcuts } from '@/components/app/shortcuts'
import { formatDate } from '@/components/issues/fields'
import { IssueView } from '@/components/issues/issue-view'
import { defaultSort, hasFilters, issuePageQuery, validateViewSearch, type IssuePage, type ViewSearch } from '@/components/issues/view-search'
import { ProjectMilestones } from '@/components/projects/milestones'
import { ArchiveProject, DeleteProject, ProjectForm, ProjectMembers } from '@/components/projects/project-form'
import '@/components/issues/issues.css'
import '@/components/projects/projects.css'

export const Route = createFileRoute('/_app/$workspace/_main/projects/$key')({
  // edit: opens the edit dialog (the roadmap's "Set dates").
  validateSearch: (search): ViewSearch & { edit?: true } => ({ ...validateViewSearch(search), ...(search.edit === true ? { edit: true as const } : {}) }),
  loaderDeps: ({ search: { edit: _, ...search } }) => ({ search }),
  loader: async (ctx) => {
    const { params } = ctx
    const path = `${workspacePath(params.workspace)}`
    const projectPath = `${path}/projects/${encodeURIComponent(params.key)}`
    const source = `${projectPath}/issues`
    const projectReq = loaderApi<Project>(ctx, projectPath)
    // ponytail: without a sort in the URL the issues wait for the project (its default sort); ship the default with
    // the issue page if that wait shows.
    const sort = ctx.deps.search.sort ?? projectReq.then(defaultSort)
    const [project, page, cycles, statuses, labels] = await Promise.all([
      projectReq,
      Promise.resolve(sort).then((s) => loaderApi<IssuePage>(ctx, `${source}?${issuePageQuery(ctx.deps.search, false, s)}`)),
      loaderApi<Cycle[]>(ctx, cyclesPath(params.workspace, params.key)),
      loaderApi<Status[]>(ctx, `${path}/statuses`),
      loaderApi<Label[]>(ctx, `${path}/labels`),
    ]).catch((e) => {
      if (e instanceof ApiError && e.status === 404) throw notFound()
      throw e
    })
    if (project.key !== params.key) {
      throw redirect({ to: '/$workspace/projects/$key', params: { workspace: params.workspace, key: project.key }, replace: true })
    }
    return { project, page, source, cycles, statuses, labels }
  },
  component: ProjectPage,
})

function ProjectPage() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const { project, page, source, cycles, statuses, labels } = Route.useLoaderData()
  const { user } = Route.useRouteContext()
  const search = Route.useSearch()
  const navigate = useNavigate({ from: Route.fullPath })
  const [editing, setEditing] = useState(Boolean(search.edit))
  const { openCreate } = useShortcuts()
  const manage = can(workspace, 'projects.manage')
  const create = can(workspace, 'issues.create') && !project.archived
  const current = cycles.find((c) => c.state === 'current')
  const empty = page.total === 0 && !hasFilters(search)

  function stopEditing() {
    setEditing(false)
    if (search.edit) void navigate({ search: ({ edit: _, ...rest }) => rest, replace: true })
  }

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
                {project.archived ? <DeleteProject workspace={workspace} project={project} /> : null}
              </>
            ) : (
              <Button variant="ghost" onClick={() => setEditing(true)}>
                Milestones
              </Button>
            )}
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
          {create && !empty ? <Button title="New issue (C)" onClick={openCreate}>New issue</Button> : null}
        </div>
        {empty ? (
          <div className="grid gap-4 p-6 bg-surface border rounded-lg justify-items-center text-center">
            <p className="text-body-sm text-secondary">No issues yet.</p>
            {create ? <Button title="New issue (C)" onClick={openCreate}>New issue</Button> : null}
          </div>
        ) : (
          <IssueView workspace={workspace} page={page} source={source} statuses={statuses} labels={labels} userId={user.id} search={search} cycles={cycles} defaultSort={defaultSort(project)} manual />
        )}
      </section>
      {manage ? (
        <Dialog open={editing} onClose={stopEditing} title="Edit project" wide>
          <ProjectForm
            workspace={workspace}
            project={project}
            onDone={stopEditing}
            aside={<ProjectMilestones workspace={workspace} project={project} manage={!project.archived} />}
          />
        </Dialog>
      ) : (
        <Dialog open={editing} onClose={stopEditing} title="Milestones">
          <ProjectMilestones workspace={workspace} project={project} manage={false} titled={false} />
        </Dialog>
      )}
    </main>
  )
}
