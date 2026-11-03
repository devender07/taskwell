import { useEffect, useRef, useState } from 'react'
import { api, projectPeoplePath, toFormErrors, type Invite, type Member, workspacePath } from '@/api'
import { subscribePaths } from '@/realtime'

export type MemberPage = { members: Member[]; invites: Invite[]; total: number; has_more: boolean }

// Page one of the workspace's members matching q (name or email); `&offset=` fetches the next.
export const memberPagePath = (slug: string, q: string) => `${workspacePath(slug)}/members?${new URLSearchParams({ limit: '50', q })}`

// Up to `limit` members whose name or email contains q, in members' order.
export const searchMembers = async (slug: string, q: string, limit = 8, signal?: AbortSignal) =>
  (await api<MemberPage>(`${workspacePath(slug)}/members?${new URLSearchParams({ limit: String(limit), q })}`, undefined, 'GET', signal)).members

export type PeopleSearch = (q: string, signal?: AbortSignal) => Promise<Member[]>

// Who can be assigned and @mentioned, searched as you type: the project's people, or any workspace member without one.
export const peopleSearch =
  (slug: string, project?: string): PeopleSearch =>
  (q, signal) =>
    project
      ? api<Member[]>(`${projectPeoplePath(slug, project)}?${new URLSearchParams({ limit: '20', q })}`, undefined, 'GET', signal)
      : searchMembers(slug, q, 20, signal)

// The members matching q, a page at a time: `first` is page one when a loader has it, else it's fetched (and
// refetched live) here. `end` goes after the list; scrolling it into view loads the next page. A new page one (a
// refetch after a change) starts the list over.
export function useMemberPages(slug: string, q: string, first?: MemberPage) {
  const path = memberPagePath(slug, q)
  const [own, setOwn] = useState<{ path: string; page: MemberPage } | null>(null)
  const [extra, setExtra] = useState<{ base: MemberPage; members: Member[]; has_more: boolean } | null>(null)
  const [error, setError] = useState<string | null>(null)
  const busy = useRef(false)
  const end = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (first) return
    let live = true
    const load = () => api<MemberPage>(path).then((page) => live && setOwn({ path, page }), (e) => live && setError(toFormErrors(e).error))
    void load()
    const off = subscribePaths([path], load)
    return () => {
      live = false
      off()
    }
  }, [path, first])

  const base = first ?? (own?.path === path ? own.page : null)
  const more = base && extra?.base === base ? extra : null
  const ids = new Set(base?.members.map((m) => m.id))
  const members = base ? [...base.members, ...(more?.members.filter((m) => !ids.has(m.id)) ?? [])] : []
  const hasMore = more ? more.has_more : !!base?.has_more
  const offset = (base?.members.length ?? 0) + (more?.members.length ?? 0)

  useEffect(() => {
    const el = end.current
    if (!el || !base || !hasMore) return
    const io = new IntersectionObserver(async ([e]) => {
      if (!e?.isIntersecting || busy.current) return
      busy.current = true
      try {
        const next = await api<MemberPage>(`${path}&offset=${offset}`)
        setExtra({ base, members: [...(more?.members ?? []), ...next.members], has_more: next.has_more })
      } catch (err) {
        setError(toFormErrors(err).error)
      }
      busy.current = false
    })
    io.observe(el)
    return () => io.disconnect()
  }, [base, more, hasMore, offset, path])

  return { members, total: base?.total ?? 0, loaded: base !== null, hasMore, end, error }
}
