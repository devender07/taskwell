import { useRouter, type AnyRouteMatch, type AnyRouter } from '@tanstack/react-router'
import { useEffect } from 'react'
import { coalescer, eventPaths, loadedPaths, matchKey, touches, type RealtimeEvent } from '@tether/shared/realtime'
import { createSocket } from '@tether/shared/socket'
import { api, getMe, setClient } from '@/api'

// The admin service's realtime socket (docs/realtime-plan.md): events name the admin API paths that changed and
// the tab reloads the route matches that loaded them.

type Message = RealtimeEvent | { type: 'hello'; client: string } | { type: 'pong' | 'resync' | 'error' }
type LoaderContext = { route: { id: string; fullPath: string }; params: Record<string, string>; deps: unknown }

const loaded = loadedPaths()
const keyOf = (m: AnyRouteMatch) => matchKey(m.routeId, m.fullPath, m.params, m.loaderDeps)

let router: AnyRouter | null = null
let client = ''

// A loader's GET, tracked so events for its path reload the match.
export function loaderApi<T>(ctx: LoaderContext, path: string) {
  loaded.add(matchKey(ctx.route.id, ctx.route.fullPath, ctx.params, ctx.deps), path)
  return api<T>(path)
}

const push = coalescer(
  (paths) => {
    const filter = (m: AnyRouteMatch) => touches(loaded.get(keyOf(m)) ?? [], paths)
    if (router?.state.matches.some(filter)) void router.invalidate({ filter })
  },
  { throttle: (p) => (p === '/stats' ? 5000 : undefined) },
)

const socket = createSocket({
  onHello(id, again) {
    client = id
    setClient(id)
    if (again) setTimeout(() => push(['/']), Math.random() * 2000) // spread a restart's refetches over 2s
  },
  onDrop() {
    client = ''
    setClient('')
  },
  onMessage(msg) {
    const m = msg as Message
    if (m.type === 'resync') push(['/'])
    else if (m.type === 'event') push(eventPaths(m, client))
  },
  // A connect that fails before `hello` may be a lapsed session: back to sign-in if so.
  async stillSignedIn() {
    if (await getMe().catch(() => true)) return true
    location.assign(`/signin?redirect=${encodeURIComponent(location.pathname + location.search)}`)
    return false
  },
})

function start(r: AnyRouter) {
  router = r
  const stop = socket.start()
  const unsubscribe = r.subscribe('onResolved', () => loaded.prune(r.state.matches.map(keyOf)))
  return () => {
    unsubscribe()
    stop()
  }
}

// Mounted once by the _app layout.
export function useRealtime() {
  const r = useRouter()
  useEffect(() => start(r), [r])
}
