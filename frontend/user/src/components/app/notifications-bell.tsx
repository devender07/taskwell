import { useParams } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { Popover } from '@tether/shared/popover'
import { BellIcon } from '@tether/shared/icons'
import { api, toFormErrors, type NotificationPage, workspacePath } from '@/api'
import { PROVIDER_NAME } from '@/components/issues/pull-requests'
import { FeedRow, notificationText, notificationTarget } from '@/components/inbox/feed-row'
import { subscribePaths } from '@/realtime'
import { useUnread } from './unread'

const SHOWN = 10

export function NotificationsBell() {
  const slug = useParams({ strict: false }).workspace
  const { count } = useUnread()
  if (!slug) return null
  return (
    <Popover
      aria-label="Notifications"
      placement="bottom-end"
      className="notifications"
      trigger={(props) => (
        <button type="button" className="navbar-bell" aria-label={count ? `Notifications, ${count} unread` : 'Notifications'} {...props}>
          <BellIcon size={20} />
          {count ? <span className="navbar-badge" aria-hidden>{count > 9 ? '9+' : count}</span> : null}
        </button>
      )}
    >
      {(close) => <Notifications key={slug} slug={slug} close={close} />}
    </Popover>
  )
}

// The 10 newest notifications, refetched live while the popover is open.
function Notifications({ slug, close }: { slug: string; close: () => void }) {
  const { count, refresh } = useUnread()
  const [page, setPage] = useState<NotificationPage | null>(null)
  const [error, setError] = useState<string | null>(null)
  const path = `${workspacePath(slug)}/notifications`

  useEffect(() => {
    const load = () => api<NotificationPage>(path).then(setPage, (e) => setError(toFormErrors(e).error))
    load()
    return subscribePaths([path], load)
  }, [path])

  async function markRead(ids: string[] | 'all') {
    setPage((p) => p && { ...p, notifications: p.notifications.map((n) => (ids === 'all' || ids.includes(n.id) ? { ...n, read: true } : n)) })
    const form = new FormData()
    if (ids === 'all') form.set('all', 'true')
    else for (const id of ids) form.append('ids', id)
    try {
      await api(`${path}/read`, form)
    } catch (e) {
      setError(toFormErrors(e).error)
    }
    refresh()
  }

  const items = page?.notifications.slice(0, SHOWN) ?? []
  return (
    <div className="grid">
      <div className="notifications-header">
        <h2 className="text-label m-0">Notifications</h2>
        {count || items.some((n) => !n.read) ? (
          <button type="button" className="notifications-action" onClick={() => markRead('all')}>
            Mark all read
          </button>
        ) : null}
      </div>
      {error ? <p role="alert" className="m-0 px-4 py-2 text-caption text-accent">{error}</p> : null}
      {!page ? (
        <p className="m-0 p-4 text-body-sm text-secondary">Loading…</p>
      ) : items.length === 0 ? (
        <p className="m-0 p-4 text-body-sm text-secondary">You're all caught up.</p>
      ) : (
        <ul className="notifications-list">
          {items.map((n) => (
            <li key={n.id} className="inbox-row">
              <FeedRow
                workspace={slug}
                {...notificationTarget(n)}
                actor={n.actor}
                actorName={n.data.via && PROVIDER_NAME[n.data.via.provider]}
                text={notificationText(n)}
                excerpt={n.data.excerpt}
                at={n.created_at}
                unread={!n.read}
                onClick={() => {
                  if (!n.read) markRead([n.id])
                  close()
                }}
              />
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

