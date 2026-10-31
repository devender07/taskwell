import { useParams } from '@tanstack/react-router'
import { createContext, use, useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { api } from '@/api'
import { connected, subscribePaths } from '@/realtime'

type Unread = { count: number; refresh: () => void }
const UnreadContext = createContext<Unread>({ count: 0, refresh() {} })
export const useUnread = () => use(UnreadContext)

// Refetches on realtime events, on focus, and every 60s as a fallback (5 minutes while the socket is up).
export function UnreadProvider({ children }: { children: ReactNode }) {
  const slug = useParams({ strict: false }).workspace
  const [state, setState] = useState<{ slug: string; count: number } | null>(null)

  const refresh = useCallback(() => {
    if (!slug) return
    api<{ count: number }>(`/workspaces/${encodeURIComponent(slug)}/notifications/unread-count`).then(
      ({ count }) => setState({ slug, count }),
      () => {},
    )
  }, [slug])

  useEffect(() => {
    if (!slug) return
    refresh()
    let ticks = 0
    // ponytail: fallback poll; remove it once the socket has proven itself.
    const timer = setInterval(() => (!connected() || ++ticks % 5 === 0) && refresh(), 60_000)
    const unsubscribe = subscribePaths([`/workspaces/${encodeURIComponent(slug)}/notifications/unread-count`], refresh)
    window.addEventListener('focus', refresh)
    return () => {
      clearInterval(timer)
      unsubscribe()
      window.removeEventListener('focus', refresh)
    }
  }, [refresh, slug])

  const count = state && state.slug === slug ? state.count : 0
  const value = useMemo(() => ({ count, refresh }), [count, refresh])
  return <UnreadContext value={value}>{children}</UnreadContext>
}
