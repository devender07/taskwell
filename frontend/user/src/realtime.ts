import { useRouter, type AnyRouteMatch, type AnyRouter } from '@tanstack/react-router'
import { useEffect } from 'react'
import { coalescer, decodeFrame, encodeFrame, eventPaths, matchKey, touches, uuidBytes, type RealtimeEvent } from '@tether/shared/realtime'
import { api, refreshAccess, sessionEnded, setClient, type PersonRef, type Workspace } from '@/api'

// One WebSocket per tab (docs/realtime-plan.md): events name the REST paths that changed and the tab refetches
// the ones it has loaded; it also carries live description editing and who's viewing an issue.

export type Viewer = PersonRef
type Handlers = {
  onOpen: () => void
  onFrame: (type: number, payload: Uint8Array) => void
  onClose: () => void
  onPresence: (viewers: Viewer[]) => void
}
type Channel = Handlers & { workspace: string; issue: string; uuid: string | null; id: Uint8Array | null }
type Message =
  | RealtimeEvent
  | { type: 'hello'; client: string }
  | { type: 'pong' | 'resync' | 'error' }
  | { type: 'collab.joined'; issue: string; id: string }
  | { type: 'collab.error'; issue: string }
  | { type: 'presence'; issue: string; viewers: Viewer[] }
type LoaderContext = { route: { id: string; fullPath: string }; params: Record<string, string>; deps: unknown }

// _app's beforeLoad fetches these; any load re-runs it.
const APP_PATHS = ['/workspaces', '/me']
const ALL = ['/']

// ponytail: one entry per route match ever loaded, never pruned; prune on navigation if long sessions grow it.
const loaded = new Map<string, Set<string>>()
const subscribers = new Set<{ paths: string[]; refetch: () => void }>()
const channels = new Set<Channel>()

let router: AnyRouter | null = null
let ws: WebSocket | null = null
let client = ''
let workspace: string | undefined
let running = false
let opened = false
let delay = 1000
let steady: ReturnType<typeof setTimeout> | undefined // resets the backoff once a socket has stayed up
let reauthed = false // the last drop was the server asking for a fresh token, not a failure
let retry: ReturnType<typeof setTimeout> | undefined
let ping: ReturnType<typeof setInterval> | undefined
let pong: ReturnType<typeof setTimeout> | undefined
let notice: string | null = null

// Events for `path` reload the loader's route match.
export function trackPath(ctx: LoaderContext, path: string) {
  const key = matchKey(ctx.route.id, ctx.route.fullPath, ctx.params, ctx.deps)
  let paths = loaded.get(key)
  if (!paths) loaded.set(key, (paths = new Set()))
  paths.add(path)
}

// A loader's GET, tracked so events for its path reload the match.
export function loaderApi<T>(ctx: LoaderContext, path: string) {
  trackPath(ctx, path)
  return api<T>(path)
}

// For data fetched outside loaders: `refetch` runs when an event touches any of `paths`. Returns the unsubscribe.
export function subscribePaths(paths: string[], refetch: () => void) {
  const sub = { paths, refetch }
  subscribers.add(sub)
  return () => void subscribers.delete(sub)
}

export const connected = () => client !== ''

// "You no longer have access to …", shown once on the home page after a workspace goes away.
export const readNotice = () => notice
export const clearNotice = () => void (notice = null)

const loadedBy = (m: AnyRouteMatch) =>
  m.routeId === '/_app' ? APP_PATHS : (loaded.get(matchKey(m.routeId, m.fullPath, m.params, m.loaderDeps)) ?? [])

// Issue lists refetch at most every 3s, so a teammate typing doesn't reload them on every save.
const LIST = /^\/workspaces\/[^/]+\/(projects\/[^/]+\/issues|my-issues)$/

const push = coalescer(
  (paths) => {
    const filter = (m: AnyRouteMatch) => touches(loadedBy(m), paths)
    router?.clearCache({ filter }) // hover-preloaded data for these paths is now stale
    if (router?.state.matches.some(filter)) void reload(router, filter, touches(APP_PATHS, paths))
    for (const s of subscribers) if (touches(s.paths, paths)) s.refetch()
  },
  { throttle: (p) => (LIST.test(p) ? 3000 : undefined) },
)

// Only after `hello`; everything sent before then is sent again on hello.
const send = (msg: object) => {
  if (client && ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify(msg))
}
const join = (ch: Channel) => send({ type: 'collab.join', workspace: ch.workspace, issue: ch.issue })
const channel = (issue: string) => [...channels].find((c) => c.issue === issue)

function onEvent(e: RealtimeEvent) {
  const paths = eventPaths(e, client)
  if (!paths.length || !router) return
  const here = workspace && (e.workspace === workspace || e.workspace === e.new_slug)
  if (e.kind === 'workspace.changed' && e.new_slug && here && workspace !== e.new_slug) {
    const rest = location.pathname.slice(`/${encodeURIComponent(workspace!)}`.length)
    void router.navigate({ href: `/${encodeURIComponent(e.new_slug)}${rest}${location.search}`, replace: true })
    return
  }
  if (e.kind === 'workspace.removed' && e.workspace === workspace) {
    const app = router.state.matches.find((m) => m.routeId === '/_app')?.context as { workspaces?: Workspace[] } | undefined
    const name = app?.workspaces?.find((w) => w.slug === workspace)?.name ?? workspace
    notice = `You no longer have access to ${name}.`
    workspace = undefined
    void router.navigate({ to: '/' })
    return
  }
  push(paths)
}

function onMessage(data: unknown) {
  if (data instanceof ArrayBuffer) {
    const frame = decodeFrame(new Uint8Array(data))
    const ch = frame && [...channels].find((c) => c.uuid === frame.id)
    if (ch) ch.onFrame(frame.type, frame.payload)
    return
  }
  let msg: Message
  try {
    msg = JSON.parse(String(data))
  } catch {
    return
  }
  switch (msg.type) {
    case 'hello':
      client = msg.client
      setClient(client)
      clearTimeout(steady)
      steady = setTimeout(() => (delay = 1000), 10_000)
      if (workspace) send({ type: 'subscribe', workspace })
      for (const ch of channels) join(ch)
      if (opened) setTimeout(() => push(ALL), reauthed ? 0 : Math.random() * 2000) // spread a restart's refetches over 2s
      opened = true
      clearInterval(ping)
      ping = setInterval(checkAlive, 25_000)
      break
    case 'pong':
      clearTimeout(pong)
      break
    case 'resync':
      push(ALL)
      break
    case 'event':
      onEvent(msg)
      break
    case 'collab.joined': {
      const ch = channel(msg.issue)
      if (!ch) {
        send({ type: 'collab.leave', issue: msg.id })
        break
      }
      ch.uuid = msg.id
      ch.id = uuidBytes(msg.id)
      ch.onOpen()
      break
    }
    case 'collab.error':
      channel(msg.issue)?.onClose()
      break
    case 'presence':
      channel(msg.issue)?.onPresence(msg.viewers)
      break
  }
}

// A ping without a pong in 10s means the socket is dead (common after an iPad sleeps): drop it and reconnect.
function checkAlive() {
  if (ws?.readyState !== WebSocket.OPEN) return
  send({ type: 'ping' })
  clearTimeout(pong)
  pong = setTimeout(() => {
    const sock = ws
    if (!sock) return
    sock.onclose = null
    sock.close()
    reauthed = false
    dropped(sock, true)
  }, 10_000)
}

function connect() {
  const sock = new WebSocket(`${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/api/realtime`)
  sock.binaryType = 'arraybuffer'
  ws = sock
  sock.onmessage = (e) => onMessage(e.data)
  // 4001: the access token expired or the sign-in may be revoked, so refresh before reconnecting, without backing off.
  sock.onclose = (e) => {
    reauthed = e.code === 4001
    dropped(sock, client !== '' && !reauthed)
    if (reauthed) delay = 1000
  }
}

function dropped(sock: WebSocket, hello: boolean) {
  if (ws !== sock) return
  ws = null
  client = ''
  setClient('')
  clearInterval(ping)
  clearTimeout(pong)
  clearTimeout(steady)
  for (const ch of channels) {
    ch.uuid = ch.id = null
    ch.onClose()
    ch.onPresence([])
  }
  if (!running) return
  retry = setTimeout(async () => {
    if (!hello && (await refreshAccess().catch(() => null))?.status === 401) return sessionEnded()
    if (running && !ws) connect()
  }, delay * (0.5 + Math.random()))
  delay = Math.min(delay * 2, 10_000)
}

function onVisible() {
  if (document.visibilityState !== 'visible' || !running) return
  if (ws) return checkAlive()
  clearTimeout(retry)
  delay = 1000
  connect()
}

function start(r: AnyRouter) {
  router = r
  running = true
  opened = false
  connect()
  document.addEventListener('visibilitychange', onVisible)
  return () => {
    running = false
    clearTimeout(retry)
    document.removeEventListener('visibilitychange', onVisible)
    const sock = ws
    if (sock) {
      sock.onclose = null
      sock.close()
      dropped(sock, true)
    }
  }
}

// After this tab's own change: reloads the page's own data (its deepest match) instead of the session and layouts,
// and drops other cached pages so they load fresh. Other tabs hear about the change over the socket.
export function reloadPage(r: AnyRouter) {
  const page = r.state.matches.at(-1)?.id
  r.clearCache()
  return reload(r, (m) => m.id === page, false)
}

// _app's beforeLoad runs on every load; while this is false it reuses the session it already has.
let reloadSession = true
export const keepSession = () => !reloadSession

async function reload(r: AnyRouter, filter: (m: AnyRouteMatch) => boolean, session: boolean) {
  reloadSession = session
  try {
    await r.invalidate({ filter })
  } finally {
    reloadSession = true
  }
}

// Mounted once by the _app layout: keeps the socket open and subscribed to the workspace in the URL.
export function useRealtime(slug: string | undefined) {
  const r = useRouter()
  useEffect(() => start(r), [r])
  useEffect(() => {
    workspace = slug
    if (slug) send({ type: 'subscribe', workspace: slug })
  }, [slug])
}

// Live editing of one issue's description over the shared socket. onOpen fires on every (re)join, followed by the
// stored updates and `ready`; onClose when the socket drops. Re-joins by itself on reconnect.
export function joinCollab(workspace: string, issue: string, handlers: Handlers) {
  const ch: Channel = { ...handlers, workspace, issue, uuid: null, id: null }
  channels.add(ch)
  join(ch)
  return {
    send(type: number, payload: Uint8Array) {
      if (!ch.id || ws?.readyState !== WebSocket.OPEN) return false
      ws.send(encodeFrame(type, ch.id, payload))
      return true
    },
    leave() {
      channels.delete(ch)
      if (ch.uuid) send({ type: 'collab.leave', issue: ch.uuid })
    },
  }
}
