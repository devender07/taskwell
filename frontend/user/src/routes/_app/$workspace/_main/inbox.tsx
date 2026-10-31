import { createFileRoute, useLoaderData, useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { Button } from '@tether/shared/button'
import { IconButton } from '@tether/shared/icon-button'
import { Tabs } from '@tether/shared/tabs'
import { api, toFormErrors, type NotificationPage } from '@/api'
import { loaderApi } from '@/realtime'
import { useUnread } from '@/components/app/unread'
import { FeedRow, notificationText } from '@/components/inbox/feed-row'

type Filter = 'all' | 'unread'

export const Route = createFileRoute('/_app/$workspace/_main/inbox')({
  validateSearch: (search): { filter?: 'unread' } => (search.filter === 'unread' ? { filter: 'unread' } : {}),
  loader: (ctx) => loaderApi<NotificationPage>(ctx, `/workspaces/${encodeURIComponent(ctx.params.workspace)}/notifications`),
  component: Inbox,
})

const filters: { value: Filter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'unread', label: 'Unread' },
]

function Inbox() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const page = Route.useLoaderData()
  const { filter = 'all' } = Route.useSearch()
  const navigate = useNavigate({ from: Route.fullPath })
  const { count, refresh } = useUnread()
  const [loaded, setLoaded] = useState({ page, slug: workspace.slug })
  const [items, setItems] = useState(page.notifications)
  const [hasMore, setHasMore] = useState(page.has_more)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const path = `/workspaces/${encodeURIComponent(workspace.slug)}/notifications`

  // A refetch replaces the first page and keeps any older pages already loaded.
  // ponytail: older pages keep their read state until the inbox is reopened.
  if (page !== loaded.page) {
    const oldest = page.notifications.at(-1)
    const keep = loaded.slug === workspace.slug && page.has_more && oldest
    const older = keep ? items.filter((n) => Date.parse(n.created_at) < Date.parse(oldest.created_at)) : []
    setLoaded({ page, slug: workspace.slug })
    setItems([...page.notifications, ...older])
    setHasMore(older.length ? hasMore : page.has_more)
  }

  const shown = filter === 'unread' ? items.filter((n) => !n.read) : items

  async function loadMore() {
    setLoading(true)
    setError(null)
    try {
      const next = await api<NotificationPage>(`${path}?before=${encodeURIComponent(items[items.length - 1]!.id)}`)
      setItems((prev) => [...prev, ...next.notifications])
      setHasMore(next.has_more)
    } catch (e) {
      setError(toFormErrors(e).error)
    }
    setLoading(false)
  }

  async function mark(read: boolean, ids: string[] | 'all') {
    const prev = items
    setItems((list) => list.map((n) => (ids === 'all' || ids.includes(n.id) ? { ...n, read } : n)))
    setError(null)
    const form = new FormData()
    if (ids === 'all') form.set('all', 'true')
    for (const id of ids === 'all' ? [] : ids) form.append('ids', id)
    try {
      await api(`${path}/${read ? 'read' : 'unread'}`, form)
    } catch (e) {
      setItems(prev)
      setError(toFormErrors(e).error)
    }
    refresh()
  }

  return (
    <main className="grid gap-6 w-full px-6 py-10" style={{ alignContent: 'start', maxWidth: 960 }}>
      <header className="flex items-end justify-between gap-4 flex-wrap">
        <h1 className="text-h2">Inbox</h1>
        <Button variant="secondary" disabled={!count && !items.some((n) => !n.read)} onClick={() => mark(true, 'all')}>
          Mark all as read
        </Button>
      </header>
      <Tabs
        aria-label="Notifications shown"
        className="w-fit"
        items={filters}
        value={filter}
        onChange={(v) => navigate({ search: v === 'unread' ? { filter: 'unread' } : {} })}
      />
      {error ? <p role="alert" className="m-0 text-caption text-accent">{error}</p> : null}
      {shown.length === 0 && !hasMore ? (
        <p className="text-body-sm text-secondary p-6 bg-surface border rounded-lg text-center">You're all caught up.</p>
      ) : (
        <section className="bg-surface border rounded-lg overflow-hidden" aria-label="Notifications">
          <ul className="grid m-0 p-0" style={{ listStyle: 'none' }}>
            {shown.map((n) => (
              <li key={n.id} className="inbox-row">
                <FeedRow
                  workspace={workspace.slug}
                  identifier={n.issue.identifier}
                  actor={n.actor}
                  text={notificationText(n)}
                  excerpt={n.data.excerpt}
                  at={n.created_at}
                  unread={!n.read}
                  onClick={() => (n.read ? undefined : mark(true, [n.id]))}
                />
                <IconButton aria-label={n.read ? 'Mark as unread' : 'Mark as read'} style={{ width: 44, height: 44 }} className="flex-noshrink" onClick={() => mark(!n.read, [n.id])}>
                  {n.read ? <UnreadIcon /> : <ReadIcon />}
                </IconButton>
              </li>
            ))}
          </ul>
          {hasMore ? (
            <div className="flex justify-center p-4">
              <Button variant="secondary" disabled={loading} onClick={loadMore}>
                {loading ? 'Loading…' : 'Load more'}
              </Button>
            </div>
          ) : null}
        </section>
      )}
    </main>
  )
}

function ReadIcon() {
  return (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <path d="M20 6 9 17l-5-5" />
    </svg>
  )
}

function UnreadIcon() {
  return (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.75" aria-hidden>
      <circle cx="12" cy="12" r="4" />
    </svg>
  )
}
