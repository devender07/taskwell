import { Link } from '@tanstack/react-router'
import { useEffect, useEffectEvent, useRef, useState } from 'react'
import { LogoMark } from '@tether/shared/logo'
import { api, toFormErrors, type NotificationPage, workspacePath } from '@/api'
import { useUnread } from '@/components/app/unread'
import { PROVIDER_NAME } from '@/components/issues/pull-requests'
import { FeedRow, notificationTarget, notificationText } from './feed-row'

export const botPath = (slug: string) => `${workspacePath(slug)}/notifications?exclude=chat`

// The Tether direct message: every notification except chat ones (those live in their chats), newest at the bottom.
// Opening it reads them, like any chat, and older ones load as you scroll up.
export function TetherBot({ slug, page }: { slug: string; page: NotificationPage }) {
  const { refresh } = useUnread()
  const [older, setOlder] = useState<NotificationPage | null>(null)
  const ids = new Set(page.notifications.map((n) => n.id))
  const items = [...page.notifications, ...(older?.notifications.filter((n) => !ids.has(n.id)) ?? [])]
  const more = older ? older.has_more : page.has_more
  const unread = items.filter((n) => !n.read).map((n) => n.id)

  const unreadKey = unread.join()
  useEffect(() => {
    if (!unreadKey) return
    const form = new FormData()
    for (const id of unreadKey.split(',')) form.append('ids', id)
    api(`${workspacePath(slug)}/notifications/read`, form).then(refresh, () => {})
  }, [slug, unreadKey, refresh])

  const [error, setError] = useState<string | null>(null)
  const scroller = useRef<HTMLDivElement>(null)
  const top = useRef<HTMLDivElement>(null)
  const loading = useRef(false)

  const loadOlder = useEffectEvent(async () => {
    const last = items.at(-1)
    if (!last || loading.current) return
    loading.current = true
    try {
      const next = await api<NotificationPage>(`${botPath(slug)}&before=${encodeURIComponent(last.id)}`)
      setOlder((o) => ({ notifications: [...(o?.notifications ?? []), ...next.notifications], has_more: next.has_more }))
      setError(null)
    } catch (e) {
      setError(toFormErrors(e).error)
    }
    loading.current = false
  })

  // The marker sits above the oldest row; scrolling within 200px of it loads the next page.
  const lastId = items.at(-1)?.id
  useEffect(() => {
    const el = top.current
    if (!el || !more || error) return
    const io = new IntersectionObserver((entries) => entries.some((e) => e.isIntersecting) && void loadOlder(), {
      root: scroller.current,
      rootMargin: '200px 0px 0px 0px',
    })
    io.observe(el)
    return () => io.disconnect()
  }, [more, lastId, error])

  return (
    <section className="chat-main" aria-label="Tether">
      <header className="chat-header">
        <Link to="/$workspace/inbox" params={{ workspace: slug }} className="chat-back text-caption text-secondary">
          Back
        </Link>
        <LogoMark size={24} />
        <h1 className="chat-title ellipsis">Tether</h1>
      </header>
      <div ref={scroller} className="tether-bot-scroll">
        {items.length === 0 ? <p className="text-body-sm text-secondary text-center m-0 p-8">You're all caught up.</p> : null}
        <ul className="m-0 p-0" style={{ listStyle: 'none' }} aria-label="Notifications">
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
              />
            </li>
          ))}
        </ul>
        {more ? (
          <div ref={top} className="text-caption text-secondary text-center py-2" role={error ? 'alert' : undefined}>
            {error ?? 'Loading older notifications…'}
          </div>
        ) : null}
      </div>
    </section>
  )
}
