import { useParams } from '@tanstack/react-router'
import { createContext, use, useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { api } from '@/api'

type Unread = { count: number; refresh: () => void }
const UnreadContext = createContext<Unread>({ count: 0, refresh() {} })
export const useUnread = () => use(UnreadContext)

// ponytail: polls every 60s and on focus; push over a socket if the inbox needs to be live.
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
    refresh()
    const timer = setInterval(refresh, 60_000)
    window.addEventListener('focus', refresh)
    return () => {
      clearInterval(timer)
      window.removeEventListener('focus', refresh)
    }
  }, [refresh])

  const count = state && state.slug === slug ? state.count : 0
  const value = useMemo(() => ({ count, refresh }), [count, refresh])
  return <UnreadContext value={value}>{children}</UnreadContext>
}
