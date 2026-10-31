import { useParams } from '@tanstack/react-router'
import { createContext, use, useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { api } from '@/api'
import { connected, subscribePaths } from '@/realtime'

// count: unread notifications (the navbar bell); chats: unread chat messages (the sidebar's Inbox).
type Unread = { count: number; chats: number; refresh: () => void }
const UnreadContext = createContext<Unread>({ count: 0, chats: 0, refresh() {} })
export const useUnread = () => use(UnreadContext)

// Refetches on realtime events, on focus, and every 60s as a fallback (5 minutes while the socket is up).
export function UnreadProvider({ children }: { children: ReactNode }) {
  const slug = useParams({ strict: false }).workspace
  const [state, setState] = useState<{ slug: string; count: number } | null>(null)
  const [chatState, setChatState] = useState<{ slug: string; count: number } | null>(null)

  const refresh = useCallback(() => {
    if (!slug) return
    const ws = `/workspaces/${encodeURIComponent(slug)}`
    api<{ count: number }>(`${ws}/notifications/unread-count`).then(({ count }) => setState({ slug, count }), () => {})
    api<{ count: number }>(`${ws}/chats/unread-count`).then(({ count }) => setChatState({ slug, count }), () => {})
  }, [slug])

  useEffect(() => {
    if (!slug) return
    refresh()
    let ticks = 0
    // ponytail: fallback poll; remove it once the socket has proven itself.
    const timer = setInterval(() => (!connected() || ++ticks % 5 === 0) && refresh(), 60_000)
    const ws = `/workspaces/${encodeURIComponent(slug)}`
    const unsubscribe = subscribePaths([`${ws}/notifications/unread-count`, `${ws}/chats/unread-count`], refresh)
    window.addEventListener('focus', refresh)
    return () => {
      clearInterval(timer)
      unsubscribe()
      window.removeEventListener('focus', refresh)
    }
  }, [refresh, slug])

  const count = state && state.slug === slug ? state.count : 0
  const chats = chatState && chatState.slug === slug ? chatState.count : 0
  const value = useMemo(() => ({ count, chats, refresh }), [count, chats, refresh])
  return <UnreadContext value={value}>{children}</UnreadContext>
}
