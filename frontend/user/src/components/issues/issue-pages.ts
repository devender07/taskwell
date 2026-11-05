import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { api, toFormErrors, type Issue } from '@/api'
import type { IssuePage } from './view-search'
import { idSet, extractDataFromArray } from '@taskwell/shared/list'

type Extra = { issues: Issue[]; offset: number; has_more: boolean; error?: string }

// Each group's issues past the first page: `page` (from the loader) has page one of every group, `more(key)` appends
// that group's next page from `source?query&in=key&offset=n`. One request per group at a time; an error stops the group.
// A new `page` (a reload after a move or a realtime event) starts every group over.
export function useIssuePages(page: IssuePage, source: string, query: string) {
  const [extra, setExtra] = useState<{ base: IssuePage; groups: Record<string, Extra> }>({ base: page, groups: {} })
  const latest = useRef({ page, extra })
  latest.current = { page, extra }
  const busy = useRef({ base: page, keys: new Set<string>() })
  const groups = extra.base === page ? extra.groups : null

  const more = useCallback(
    async (key: string) => {
      const first = page.groups.find((g) => g.key === key)
      const own = latest.current.extra.base === page ? latest.current.extra.groups[key] : undefined
      if (busy.current.base !== page) busy.current = { base: page, keys: new Set() }
      const keys = busy.current.keys
      if (!first || keys.has(key) || own?.error || !(own ? own.has_more : first.has_more)) return
      keys.add(key)
      const offset = first.issues.length + (extractDataFromArray(own, 'offset', 0))
      const update = (fn: (e: Extra) => Extra) => {
        if (latest.current.page !== page) return
        setExtra((prev) => {
          const groups = prev.base === page ? prev.groups : {}
          return { base: page, groups: { ...groups, [key]: fn(groups[key] ?? { issues: [], offset: 0, has_more: true }) } }
        })
      }
      try {
        const next = await api<{ issues: Issue[]; has_more: boolean }>(`${source}?${query}&in=${encodeURIComponent(key)}&offset=${offset}`)
        update((e) => {
          const ids = idSet(e.issues)
          return { issues: [...e.issues, ...next.issues.filter((i) => !ids.has(i.id))], offset: e.offset + next.issues.length, has_more: next.has_more }
        })
      } catch (err) {
        update((e) => ({ ...e, error: toFormErrors(err).error ?? "Couldn't load more issues." }))
      } finally {
        keys.delete(key)
      }
    },
    [page, source, query],
  )

  const merged = useMemo<IssuePage>(() => {
    if (!groups) return page
    return {
      ...page,
      groups: page.groups.map((g) => {
        const e = groups[g.key]
        if (!e) return g
        const ids = idSet(g.issues)
        return { ...g, issues: [...g.issues, ...e.issues.filter((i) => !ids.has(i.id))], has_more: e.has_more }
      }),
    }
  }, [page, groups])

  return { page: merged, more, error: (key: string) => groups?.[key]?.error }
}

// A sentinel for the end of a group: scrolling it into view calls `more(key)`; null disarms it. `scroller` observes it
// against its parent (a board column scrolls on its own) instead of the window. `loaded` re-arms it after each page, so
// a sentinel still in view loads the next one too.
export function usePageEnd<T extends HTMLElement>(more: ((key: string) => void) | null, key: string, loaded: number, scroller = false) {
  const ref = useRef<T>(null)
  useEffect(() => {
    const el = ref.current
    if (!el || !more) return
    const io = new IntersectionObserver(([e]) => e?.isIntersecting && more(key), { root: scroller ? el.parentElement : null, rootMargin: '200px' })
    io.observe(el)
    return () => io.disconnect()
  }, [more, key, loaded, scroller])
  return ref
}
