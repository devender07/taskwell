import { Link } from '@tanstack/react-router'
import { useActionState, useEffect, useId, useState, type ReactNode } from 'react'
import { Button } from '@tether/shared/button'
import { api, can, toFormErrors, type ActivityItem, type ActivityPage, type Issue, type NotificationPage, type Project, type ProjectStats, type Status, type Workspace } from '@/api'
import { useShortcuts } from '@/components/app/shortcuts'
import { useUnread } from '@/components/app/unread'
import { FormError } from '@/components/auth/auth-card'
import { FeedRow, IssueName, notificationText } from '@/components/inbox/feed-row'
import { eventText } from '@/components/issues/event-text'
import { StatusIcon, formatDate, overdue, priorityLabel } from '@/components/issues/fields'
import { groupIssues } from '@/components/issues/view-search'
import { localDate, readRecent } from './home'
import '@/components/issues/issues.css'
import './home.css'

function Card({ title, action, className = '', children }: { title: string; action?: ReactNode; className?: string; children: ReactNode }) {
  const id = useId()
  return (
    <section aria-labelledby={id} className={`bg-surface border rounded-lg overflow-hidden ${className}`} style={{ minWidth: 0 }}>
      <header className="home-card-header">
        <h2 id={id} className="text-label m-0">{title}</h2>
        {action ? <div className="flex items-center gap-3">{action}</div> : null}
      </header>
      {children}
    </section>
  )
}

function Empty({ text, children }: { text: string; children?: ReactNode }) {
  return (
    <div className="grid gap-4 px-6 py-6" style={{ justifyItems: 'start' }}>
      <p className="text-body-sm text-secondary m-0">{text}</p>
      {children}
    </div>
  )
}

function IssueLine({ workspace, identifier, children }: { workspace: string; identifier: string; children: ReactNode }) {
  return (
    <Link to="/$workspace/issue/$identifier" params={{ workspace, identifier }} className="issue-row">
      {children}
    </Link>
  )
}

export function NeedsAttention({ workspace, issues }: { workspace: string; issues: Issue[] }) {
  const today = localDate(new Date())
  return (
    <Card
      title="Needs attention"
      action={
        <Link to="/$workspace/my-issues" params={{ workspace }} search={{ sort: 'due' }} className="home-link">
          View all
        </Link>
      }
    >
      {issues.length === 0 ? (
        <Empty text="Nothing due this week." />
      ) : (
        <ul className="home-list">
          {issues.slice(0, 8).map((i) => (
            <li key={i.id}>
              <IssueLine workspace={workspace} identifier={i.identifier}>
                <StatusIcon category={i.status.category} label={i.status.name} />
                <span className="issue-id flex-noshrink">{i.identifier}</span>
                <span className="text-label text-primary ellipsis flex-grow">{i.title}</span>
                <span className={`text-caption flex-noshrink ${overdue(i.due_date, i.status.category) ? 'text-accent' : 'text-secondary'}`}>
                  {i.due_date === today ? 'Today' : formatDate(i.due_date!)}
                </span>
              </IssueLine>
            </li>
          ))}
        </ul>
      )}
    </Card>
  )
}

type AssignedProps = { workspace: Workspace; issues: Issue[]; statuses: Status[]; others: boolean }

export function AssignedToMe({ workspace, issues, statuses, others }: AssignedProps) {
  const { openCreate } = useShortcuts()
  const groups = groupIssues(issues, 'status', { statuses, members: [], userId: '' }).filter((g) => g.issues.length > 0)
  return (
    <Card
      title="Assigned to me"
      action={
        <Link to="/$workspace/my-issues" params={{ workspace: workspace.slug }} className="home-link">
          View all
        </Link>
      }
    >
      {groups.length === 0 ? (
        <Empty text={others ? 'Nothing else assigned to you.' : 'Nothing assigned to you.'}>
          {can(workspace, 'issues.create') ? (
            <Button variant="secondary" onClick={openCreate}>
              Create issue
            </Button>
          ) : null}
        </Empty>
      ) : (
        groups.map((g) => (
          <div key={g.key} role="group" aria-label={`${g.label}, ${g.issues.length}`}>
            <div className="issue-group-header">
              {g.status ? <StatusIcon category={g.status.category} /> : null}
              <span className="text-label">{g.label}</span>
              <span className="text-caption text-secondary">{g.issues.length}</span>
            </div>
            <ul className="home-list">
              {g.issues.slice(0, 5).map((i) => (
                <li key={i.id}>
                  <IssueLine workspace={workspace.slug} identifier={i.identifier}>
                    <span className="issue-id flex-noshrink">{i.identifier}</span>
                    <span className="text-label text-primary ellipsis flex-grow">{i.title}</span>
                    {i.priority ? <span className="text-caption text-secondary flex-noshrink">{priorityLabel(i.priority)}</span> : null}
                  </IssueLine>
                </li>
              ))}
            </ul>
          </div>
        ))
      )}
    </Card>
  )
}

export function Inbox({ workspace, page }: { workspace: string; page: NotificationPage }) {
  const { count, refresh } = useUnread()
  const [cleared, setCleared] = useState<NotificationPage | null>(null)
  const [error, setError] = useState<string | null>(null)
  const unread = cleared === page ? [] : page.notifications.filter((n) => !n.read).slice(0, 5)
  const path = `/workspaces/${encodeURIComponent(workspace)}/notifications/read`

  async function markAll() {
    setCleared(page)
    setError(null)
    const form = new FormData()
    form.set('all', 'true')
    try {
      await api(path, form)
    } catch (e) {
      setCleared(null)
      setError(toFormErrors(e).error)
    }
    refresh()
  }

  function open(id: string) {
    const form = new FormData()
    form.append('ids', id)
    api(path, form).then(refresh, () => {})
  }

  return (
    <Card
      title="Inbox"
      action={
        <>
          {unread.length || (count && cleared !== page) ? (
            <button type="button" className="home-link home-link-button" onClick={markAll}>
              Mark all read
            </button>
          ) : null}
          <Link to="/$workspace/inbox" params={{ workspace }} className="home-link">
            View inbox
          </Link>
        </>
      }
    >
      {error ? <div className="px-6 pt-3"><FormError>{error}</FormError></div> : null}
      {unread.length === 0 ? (
        <Empty text="You're all caught up." />
      ) : (
        <ul className="home-list">
          {unread.map((n) => (
            <li key={n.id}>
              <FeedRow
                workspace={workspace}
                identifier={n.issue.identifier}
                actor={n.actor}
                text={notificationText(n)}
                excerpt={n.data.excerpt}
                at={n.created_at}
                unread
                onClick={() => open(n.id)}
              />
            </li>
          ))}
        </ul>
      )}
    </Card>
  )
}

const at = (x: { created_at: string }) => Date.parse(x.created_at)

function activityText(item: ActivityItem) {
  const subject = <IssueName issue={item.issue} />
  if (item.type === 'comment') return <>commented on {subject}</>
  return eventText(item, (i) => <span className="text-primary">{i.identifier}</span>, subject)
}

// `page` is the newest page (replaced on every realtime refetch); older pages load on demand and stay below it.
export function Activity({ workspace, page }: { workspace: string; page: ActivityPage }) {
  const [newest, setNewest] = useState(page)
  const [older, setOlder] = useState<ActivityItem[]>([])
  const [olderMore, setOlderMore] = useState<boolean | null>(null)
  if (page !== newest) {
    setNewest(page)
    if (!page.has_more) {
      setOlder([])
      setOlderMore(null)
    } else if (older.length) {
      // Items pushed off the newest page by new activity move down, so there's no gap above the older pages.
      const edge = Math.min(...page.items.map(at))
      setOlder([...newest.items, ...older].filter((x) => at(x) < edge))
    }
  }
  const items = [...page.items, ...older]

  const [error, loadMore, loading] = useActionState(async () => {
    const before = items.at(-1)?.created_at
    if (!before) return null
    try {
      const next = await api<ActivityPage>(`/workspaces/${encodeURIComponent(workspace)}/activity?before=${encodeURIComponent(before)}`)
      setOlder((o) => [...o, ...next.items])
      setOlderMore(next.has_more)
    } catch (e) {
      return toFormErrors(e).error
    }
    return null
  }, null)

  return (
    <Card title="Recent activity" className="home-activity">
      {items.length === 0 ? (
        <Empty text="No activity yet." />
      ) : (
        <ul className="home-list">
          {items.map((item) => {
            const text = activityText(item)
            return text ? (
              <li key={`${item.type}:${item.id}`}>
                <FeedRow
                  workspace={workspace}
                  identifier={item.issue.identifier}
                  actor={item.actor}
                  text={text}
                  excerpt={item.type === 'comment' ? item.excerpt : undefined}
                  at={item.created_at}
                />
              </li>
            ) : null
          })}
        </ul>
      )}
      {(olderMore ?? page.has_more) ? (
        <form action={loadMore} className="grid gap-2 px-6 py-3" style={{ justifyItems: 'start', borderTop: '1px solid var(--border)' }}>
          <Button type="submit" variant="ghost" disabled={loading}>
            {loading ? 'Loading…' : 'Show more'}
          </Button>
          <FormError>{error}</FormError>
        </form>
      ) : null}
    </Card>
  )
}

export function RecentlyViewed({ workspace, issues }: { workspace: string; issues: Issue[] }) {
  const [recent, setRecent] = useState(() => readRecent(workspace))
  useEffect(() => {
    const onStorage = (e: StorageEvent) => e.key === `tether:recent:${workspace}` && setRecent(readRecent(workspace))
    addEventListener('storage', onStorage)
    return () => removeEventListener('storage', onStorage)
  }, [workspace])
  const live = new Map(issues.map((i) => [i.identifier, i]))

  return (
    <Card title="Recently viewed">
      {recent.length === 0 ? (
        <Empty text="Issues you open will show up here." />
      ) : (
        <ul className="home-list">
          {recent.slice(0, 6).map((r) => {
            const issue = live.get(r.identifier)
            return (
              <li key={r.identifier}>
                <IssueLine workspace={workspace} identifier={r.identifier}>
                  {issue ? <StatusIcon category={issue.status.category} label={issue.status.name} /> : null}
                  <span className="issue-id flex-noshrink">{r.identifier}</span>
                  <span className="text-label text-primary ellipsis flex-grow">{issue?.title ?? r.title}</span>
                </IssueLine>
              </li>
            )
          })}
        </ul>
      )}
    </Card>
  )
}

export function Projects({ workspace, projects, stats }: { workspace: Workspace; projects: Project[]; stats: ProjectStats[] }) {
  const byKey = new Map(stats.map((s) => [s.key, s]))
  return (
    <Card title="Projects">
      {projects.length === 0 ? (
        <Empty text="No projects yet.">
          {can(workspace, 'projects.manage') ? (
            <Link to="/$workspace/projects" params={{ workspace: workspace.slug }} className="home-link">
              Create a project
            </Link>
          ) : null}
        </Empty>
      ) : (
        <ul className="home-list">
          {projects.map((p) => {
            const s = byKey.get(p.key) ?? { open: 0, done: 0, overdue: 0 }
            const total = s.open + s.done
            return (
              <li key={p.id}>
                <Link to="/$workspace/projects/$key" params={{ workspace: workspace.slug, key: p.key }} className="home-project">
                  <span className="flex items-center gap-3" style={{ minWidth: 0 }}>
                    <span className="text-label text-primary ellipsis flex-grow">{p.name}</span>
                    <span className="issue-id flex-noshrink">{p.key}</span>
                  </span>
                  <span
                    role="progressbar"
                    aria-label={`${p.name} progress`}
                    aria-valuemin={0}
                    aria-valuemax={total}
                    aria-valuenow={s.done}
                    aria-valuetext={`${s.done} of ${total} done`}
                    className="home-bar"
                  >
                    <span style={{ width: total ? `${(s.done / total) * 100}%` : 0 }} />
                  </span>
                  <span className="flex items-center gap-2 text-caption text-secondary flex-wrap">
                    <span>
                      {s.open} open · {s.done} done
                    </span>
                    {s.overdue > 0 ? <span className="text-accent">{s.overdue} overdue</span> : null}
                  </span>
                </Link>
              </li>
            )
          })}
        </ul>
      )}
    </Card>
  )
}
