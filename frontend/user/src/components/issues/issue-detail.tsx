import { Link, notFound } from '@tanstack/react-router'
import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Button } from '@tether/shared/button'
import { ApiError, can, type Issue, type Label, type Project, type Status, type Timeline as TimelineData, type User, type Workspace, workspacePath } from '@/api'
import { pruneRecent, recordRecent } from '@/components/home/home'
import { Description, Viewers } from '@/components/issues/description'
import { DeleteIssue, EditIssueForm } from '@/components/issues/issue-form'
import { IssueProperties } from '@/components/issues/issue-properties'
import { PullRequests } from '@/components/issues/pull-requests'
import { IssueFeedback } from '@/components/feedback/issue-feedback'
import { Relations, SubIssues } from '@/components/issues/relations'
import { Timeline } from '@/components/issues/timeline'
import { TestsSection } from '@/components/tests/tests'
import { WatchButton } from '@/components/issues/watch-button'
import { GitMenu } from '@/components/issues/git-menu'
import { peopleSearch } from '@/components/workspace/member-pages'
import { loaderApi, type LoaderContext, type Viewer } from '@/realtime'
import '@/components/issues/issues.css'

export type IssueData = { issue: Issue; project: Project; statuses: Status[]; labels: Label[]; timeline: TimelineData }

// Everything the issue page shows; the issue page and the inbox preview both load it. A 404 prunes Recently viewed.
export async function loadIssue(ctx: LoaderContext, workspace: string, identifier: string): Promise<IssueData> {
  const path = `${workspacePath(workspace)}`
  const key = identifier.replace(/-.*/, '').toUpperCase()
  const [issue, project, statuses, labels, timeline] = await Promise.all([
    loaderApi<Issue>(ctx, `${path}/issues/${encodeURIComponent(identifier)}`),
    loaderApi<Project>(ctx, `${path}/projects/${encodeURIComponent(key)}`),
    loaderApi<Status[]>(ctx, `${path}/statuses`),
    loaderApi<Label[]>(ctx, `${path}/labels`),
    loaderApi<TimelineData>(ctx, `${path}/issues/${encodeURIComponent(identifier)}/timeline`),
  ]).catch((e) => {
    if (e instanceof ApiError && e.status === 404) {
      pruneRecent(workspace, identifier)
      throw notFound()
    }
    throw e
  })
  return { issue, project, statuses, labels, timeline }
}

export function IssueDetail({ workspace, user, data, actions }: { workspace: Workspace; user: User; data: IssueData; actions?: ReactNode }) {
  const { issue, project, statuses, labels, timeline } = data
  const people = useMemo(() => peopleSearch(workspace.slug, project.key), [workspace.slug, project.key])
  const [editing, setEditing] = useState(false)
  const [viewers, setViewers] = useState<Viewer[]>([])
  const create = can(workspace, 'issues.create') && !project.archived
  useEffect(() => recordRecent(workspace.slug, { identifier: issue.identifier, title: issue.title }), [workspace.slug, issue.identifier, issue.title])

  return (
    <div className="issue-detail grid gap-6" style={{ alignContent: 'start' }}>
      <div className="flex items-center justify-between gap-4">
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
        {actions}
      </div>
      <div className="issue-layout">
        <article className="grid gap-6" style={{ minWidth: 0 }}>
          {editing ? (
            <EditIssueForm workspace={workspace} issue={issue} onDone={() => setEditing(false)} />
          ) : (
            <header className="flex items-start justify-between gap-4">
              <h1 className="text-h2" style={{ overflowWrap: 'anywhere' }}>{issue.title}</h1>
              <div className="flex items-start gap-2 flex-noshrink">
                <Viewers viewers={viewers} />
                <WatchButton key={issue.id} workspace={workspace} issue={issue} />
                <GitMenu issue={issue} />
                {issue.can_edit ? (
                  <Button variant="secondary" onClick={() => setEditing(true)}>
                    Edit
                  </Button>
                ) : null}
              </div>
            </header>
          )}
          <Description key={`description-${issue.id}`} workspace={workspace} issue={issue} user={user} people={people} onViewers={setViewers} />
          <TestsSection key={`tests-${issue.id}`} workspace={workspace} issue={issue} />
          {issue.parent ? null : (
            <SubIssues workspace={workspace} issue={issue} project={project} create={create} statuses={statuses} labels={labels} people={people} />
          )}
          <Relations key={issue.id} workspace={workspace} issue={issue} />
          <PullRequests issue={issue} />
          <IssueFeedback key={`feedback-${issue.id}`} workspace={workspace} identifier={issue.identifier} />
          <Timeline key={`timeline-${issue.id}`} workspace={workspace} issue={issue} project={project} timeline={timeline} people={people} />
        </article>
        <aside className="issue-aside grid gap-4">
          <IssueProperties key={issue.id} workspace={workspace} issue={issue} statuses={statuses} labels={labels} people={people} />
          {issue.can_delete ? <DeleteIssue workspace={workspace} issue={issue} /> : null}
        </aside>
      </div>
    </div>
  )
}
