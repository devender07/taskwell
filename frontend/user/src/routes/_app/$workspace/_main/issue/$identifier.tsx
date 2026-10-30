import { Link, createFileRoute, notFound, redirect, useLoaderData } from '@tanstack/react-router'
import { useState } from 'react'
import { Button } from '@tether/shared/button'
import { ApiError, api, can, type Issue, type Label, type Member, type Project, type Status } from '@/api'
import { Description } from '@/components/issues/description'
import { DeleteIssue, EditIssueForm } from '@/components/issues/issue-form'
import { IssueProperties } from '@/components/issues/issue-properties'
import { Relations, SubIssues } from '@/components/issues/relations'
import '@/components/issues/issues.css'

export const Route = createFileRoute('/_app/$workspace/_main/issue/$identifier')({
  loader: async ({ params }) => {
    const path = `/workspaces/${encodeURIComponent(params.workspace)}`
    const key = params.identifier.replace(/-.*/, '').toUpperCase()
    const data = await Promise.all([
      api<Issue>(`${path}/issues/${encodeURIComponent(params.identifier)}`),
      api<Project>(`${path}/projects/${encodeURIComponent(key)}`),
      api<Status[]>(`${path}/statuses`),
      api<Label[]>(`${path}/labels`),
      api<{ members: Member[] }>(`${path}/members`),
    ]).catch((e) => {
      if (e instanceof ApiError && e.status === 404) throw notFound()
      throw e
    })
    const [issue, project, statuses, labels, { members }] = data
    // An old project key or lowercase ID still resolves; show the canonical URL.
    if (issue.identifier !== params.identifier) {
      throw redirect({ to: '/$workspace/issue/$identifier', params: { workspace: params.workspace, identifier: issue.identifier }, replace: true })
    }
    return { issue, project, statuses, labels, members }
  },
  component: IssuePage,
})

function IssuePage() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const { user } = Route.useRouteContext()
  const { issue, project, statuses, labels, members } = Route.useLoaderData()
  const [editing, setEditing] = useState(false)
  const create = can(workspace, 'issues.create') && !project.archived

  return (
    <main className="grid gap-6 w-full px-6 py-10" style={{ alignContent: 'start' }}>
      <nav aria-label="Breadcrumb" className="flex items-center gap-2 text-caption text-secondary" style={{ minWidth: 0 }}>
        <Link to="/$workspace/projects/$key" params={{ workspace: workspace.slug, key: issue.project.key }} className="text-secondary ellipsis">
          {issue.project.name}
        </Link>
        <span aria-hidden>›</span>
        {issue.parent ? (
          <>
            <Link to="/$workspace/issue/$identifier" params={{ workspace: workspace.slug, identifier: issue.parent.identifier }} className="issue-id flex-noshrink">
              {issue.parent.identifier}
            </Link>
            <span aria-hidden>›</span>
          </>
        ) : null}
        <span className="issue-id flex-noshrink" aria-current="page">{issue.identifier}</span>
      </nav>
      <div className="issue-layout">
        <article className="grid gap-6" style={{ minWidth: 0 }}>
          {editing ? (
            <EditIssueForm workspace={workspace} issue={issue} onDone={() => setEditing(false)} />
          ) : (
            <header className="flex items-start justify-between gap-4">
              <h1 className="text-h2" style={{ overflowWrap: 'anywhere' }}>{issue.title}</h1>
              {issue.can_edit ? (
                <Button variant="secondary" className="flex-noshrink" onClick={() => setEditing(true)}>
                  Edit
                </Button>
              ) : null}
            </header>
          )}
          <Description key={`description-${issue.id}`} workspace={workspace} issue={issue} user={user} members={members} />
          {issue.parent ? null : (
            <SubIssues workspace={workspace} issue={issue} project={project} create={create} statuses={statuses} labels={labels} members={members} />
          )}
          <Relations key={issue.id} workspace={workspace} issue={issue} />
        </article>
        <aside className="grid gap-4">
          <IssueProperties key={issue.id} workspace={workspace} issue={issue} statuses={statuses} labels={labels} members={members} />
          {issue.can_delete ? <DeleteIssue workspace={workspace} issue={issue} /> : null}
        </aside>
      </div>
    </main>
  )
}
