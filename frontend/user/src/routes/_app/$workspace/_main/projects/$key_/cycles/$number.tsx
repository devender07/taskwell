import { createFileRoute, notFound, redirect, useLoaderData, useRouter } from '@tanstack/react-router'
import { BackLink } from '@taskwell/shared/back-link'
import { startTransition, useActionState, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Dialog } from '@taskwell/shared/dialog'
import { IconButton } from '@taskwell/shared/icon-button'
import { Popover } from '@taskwell/shared/popover'
import { SmartAutocomplete } from '@taskwell/shared/smart-autocomplete'
import { MoreIcon } from '@taskwell/shared/icons'
import { ApiError, burndownPath, can, cyclesPath, rolloverCycle, type Burndown, toFormErrors, type Cycle, type Issue, type Label, type Project, type Status, type Workspace, workspacePath, requireFeature } from '@/api'
import { loaderApi, reloadPage, trackPath } from '@/realtime'
import { peopleSearch } from '@/components/workspace/member-pages'
import { FormError } from '@/components/auth/auth-card'
import { BurndownChart, CycleForm, CycleProgress, DeleteCycle, PointsDone, STATE_LABEL } from '@/components/cycles/cycles'
import { formatRange } from '@/components/issues/fields'
import { CreateIssueForm } from '@/components/issues/issue-form'
import { IssueView } from '@/components/issues/issue-view'
import { defaultSort, issuePageQuery, validateViewSearch, type IssuePage } from '@/components/issues/view-search'
import '@/components/issues/issues.css'
import '@/components/projects/projects.css'

export const Route = createFileRoute('/_app/$workspace/_main/projects/$key_/cycles/$number')({
  beforeLoad: requireFeature('cycles'),
  validateSearch: validateViewSearch,
  loaderDeps: ({ search }) => ({ search }),
  loader: async (ctx) => {
    const { params } = ctx
    if (!/^\d+$/.test(params.number)) throw notFound()
    const path = `${workspacePath(params.workspace)}`
    const projectPath = `${path}/projects/${encodeURIComponent(params.key)}`
    const list = cyclesPath(params.workspace, params.key)
    const source = `${list}/${params.number}/issues`
    trackPath(ctx, `${projectPath}/issues`) // status changes move progress and the board
    const projectReq = loaderApi<Project>(ctx, projectPath)
    const sort = ctx.deps.search.sort ?? projectReq.then(defaultSort)
    const [project, { cycle, issues }, page, cycles, statuses, labels, burndown] = await Promise.all([
      projectReq,
      loaderApi<{ cycle: Cycle; issues: Issue[] }>(ctx, `${list}/${params.number}`),
      Promise.resolve(sort).then((s) => loaderApi<IssuePage>(ctx, `${source}?${issuePageQuery(ctx.deps.search, false, s)}`)),
      loaderApi<Cycle[]>(ctx, list),
      loaderApi<Status[]>(ctx, `${path}/statuses`),
      loaderApi<Label[]>(ctx, `${path}/labels`),
      loaderApi<Burndown>(ctx, burndownPath(params.workspace, params.key, params.number)),
    ]).catch((e) => {
      if (e instanceof ApiError && e.status === 404) throw notFound()
      throw e
    })
    if (project.key !== params.key) {
      throw redirect({
        to: '/$workspace/projects/$key/cycles/$number',
        params: { workspace: params.workspace, key: project.key, number: params.number },
        search: true,
        replace: true,
      })
    }
    return { project, cycle, issues, page, source, cycles, statuses, labels, burndown }
  },
  component: CyclePage,
})

function CyclePage() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const { project, cycle, issues, page, source, cycles, statuses, labels, burndown } = Route.useLoaderData()
  const { user } = Route.useRouteContext()
  const search = Route.useSearch()
  const [dialog, setDialog] = useState<'edit' | 'delete' | 'issue' | null>(null)
  const close = () => setDialog(null)
  const manage = can(workspace, 'cycles.manage') && !project.archived
  const create = can(workspace, 'issues.create') && !project.archived
  const { total, completed, canceled } = cycle.progress
  const open = total - completed - canceled

  return (
    <main className="grid gap-6 w-full px-6 py-10" style={{ alignContent: 'start' }}>
      <header className="page-top grid gap-2">
        <BackLink to="/$workspace/projects/$key/cycles" params={{ workspace: workspace.slug, key: project.key }}>
          {project.name} cycles
        </BackLink>
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div className="flex flex-wrap items-center gap-3" style={{ minWidth: 0 }}>
            <h1 className="text-h2 ellipsis">{cycle.name}</h1>
            <span className="project-tag flex-noshrink">{STATE_LABEL[cycle.state]}</span>
            <span className="text-body-sm text-secondary">{formatRange(cycle)}</span>
          </div>
          <div className="flex items-center gap-3">
            {create ? <Button onClick={() => setDialog('issue')}>New issue</Button> : null}
            {manage ? (
              <Popover
                aria-label="Cycle actions"
                placement="bottom-end"
                trigger={(props) => (
                  <IconButton aria-label="Cycle actions" tooltip="Cycle actions" {...props}>
                    <MoreIcon size={20} />
                  </IconButton>
                )}
              >
                {(closeMenu) => (
                  <div style={{ width: 160 }}>
                    <button type="button" className="popover-item" onClick={() => (closeMenu(), setDialog('edit'))}>
                      Edit
                    </button>
                    <button type="button" className="popover-item" onClick={() => (closeMenu(), setDialog('delete'))}>
                      Delete
                    </button>
                  </div>
                )}
              </Popover>
            ) : null}
          </div>
        </div>
        <div className="grid gap-1" style={{ maxWidth: 320 }}>
          <CycleProgress cycle={cycle} />
          <PointsDone cycle={cycle} />
        </div>
      </header>
      <section className="p-6 bg-surface border rounded-lg" aria-label="Burndown">
        <BurndownChart cycle={cycle} data={burndown} />
      </section>
      {cycle.state === 'past' && open > 0 ? (
        <Rollover workspace={workspace} project={project} cycle={cycle} cycles={cycles} open={open} manage={manage} />
      ) : null}
      {issues.length === 0 ? (
        <div className="grid gap-4 p-6 bg-surface border rounded-lg justify-items-center text-center">
          <p className="text-body-sm text-secondary">No issues in this cycle.</p>
        </div>
      ) : (
        <IssueView workspace={workspace} page={page} source={source} statuses={statuses} labels={labels} userId={user.id} search={search} defaultSort={defaultSort(project)} manual />
      )}
      {create ? (
        <Dialog open={dialog === 'issue'} onClose={close} title="New issue" wide>
          <CreateIssueForm workspace={workspace} project={project} cycle={cycle} statuses={statuses} labels={labels} people={peopleSearch(workspace.slug, project.key)} />
        </Dialog>
      ) : null}
      {manage ? (
        <>
          <Dialog open={dialog === 'edit'} onClose={close} title="Edit cycle">
            <CycleForm workspace={workspace} project={project} cycles={cycles.filter((c) => c.id !== cycle.id)} cycle={cycle} onDone={close} />
          </Dialog>
          <DeleteCycle workspace={workspace} project={project} cycle={cycle} open={dialog === 'delete'} onClose={close} />
        </>
      ) : null}
    </main>
  )
}

type RolloverProps = { workspace: Workspace; project: Project; cycle: Cycle; cycles: Cycle[]; open: number; manage: boolean }

function Rollover({ workspace, project, cycle, cycles, open, manage }: RolloverProps) {
  const router = useRouter()
  const targets = cycles.filter((c) => c.state !== 'past').map((c) => ({ value: String(c.number), label: c.name, hint: formatRange(c) }))
  const [error, move, pending] = useActionState(async (_prev: string | null, to: number) => {
    try {
      await rolloverCycle(workspace.slug, project.key, cycle.number, to)
    } catch (e) {
      const { errors, error } = toFormErrors(e)
      return errors.to ?? error
    }
    await reloadPage(router)
    return null
  }, null)

  return (
    <section className="grid gap-3 p-4 bg-surface border rounded-lg" aria-label="Unfinished issues">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <p className="text-body-sm m-0">{open === 1 ? "1 issue isn't done." : `${open} issues aren't done.`}</p>
        {manage && targets.length > 0 ? (
          <div style={{ width: 240 }}>
            <SmartAutocomplete
              aria-label="Move to…"
              placeholder="Move to…"
              options={targets}
              value={null}
              disabled={pending}
              onChange={(o) => o && startTransition(() => move(Number(o.value)))}
            />
          </div>
        ) : null}
      </div>
      <FormError>{error}</FormError>
    </section>
  )
}
