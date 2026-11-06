import { extractDataFromArray } from '@taskwell/shared/list'
import { useRouter, type AnyRouteMatch, type AnyRouter } from '@tanstack/react-router'
import { useEffect } from 'react'
import { coalescer, decodeFrame, encodeFrame, eventPaths, loadedPaths, matchKey, touches, uuidBytes, type RealtimeEvent } from '@taskwell/shared/realtime'
import { createSocket } from '@taskwell/shared/socket'
import { api, homePath, refreshAccess, sessionEnded, setClient, type PersonRef, type Workspace } from '@/api'

// One WebSocket per tab (docs/realtime-plan.md): events name the REST paths that changed and the tab refetches
// the ones it has loaded; it also carries live description editing and who's viewing an issue.

export type Viewer = PersonRef
type Handlers = {
  onOpen: () => void
  onFrame: (type: number, payload: Uint8Array) => void
  onClose: () => void
  onPresence: (viewers: Viewer[]) => void
}
export type CollabKind = 'issue' | 'note'
type Channel = Handlers & { workspace: string; kind: CollabKind; key: string; uuid: string | null; id: Uint8Array | null }
type Message =
  | RealtimeEvent
  | { type: 'hello'; client: string }
  | { type: 'pong' | 'resync' | 'error' }
  | { type: 'collab.joined'; issue?: string; note?: string; id: string }
  | { type: 'collab.error'; issue?: string; note?: string }
  | { type: 'presence'; issue: string; viewers: Viewer[] }
  | Typing
export type Typing = { type: 'typing'; chat: string; thread: string | null; user: PersonRef }
export type LoaderContext = { route: { id: string; fullPath: string }; params: Record<string, string>; deps: unknown }

// _app's beforeLoad fetches these; any load re-runs it.
const APP_PATHS = ['/workspaces', '/me']
const ALL = ['/']

const loaded = loadedPaths()
const subscribers = new Set<{ paths: string[]; refetch: () => void }>()
const channels = new Set<Channel>()
const typists = new Set<(t: Typing) => void>()

let router: AnyRouter | null = null
let client = ''
let workspace: string | undefined
let notice: string | null = null

// Events for `path` reload the loader's route match.
export function trackPath(ctx: LoaderContext, path: string) {
  loaded.add(matchKey(ctx.route.id, ctx.route.fullPath, ctx.params, ctx.deps), path)
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
  m.routeId === '/_app' ? APP_PATHS : (loaded.get(keyOf(m)) ?? [])
const keyOf = (m: AnyRouteMatch) => matchKey(m.routeId, m.fullPath, m.params, m.loaderDeps)

// Issue lists, notes, and Home's activity and project counts refetch at most every 3s, so someone typing (a live
// note saves every second) doesn't reload them on every save.
const LIST = /^\/workspaces\/[^/]+\/(projects\/[^/]+\/issues|my-issues|watching|activity|project-stats|notes)$/
// The chat list and its unread count refetch at most every second, so a busy chat doesn't reload them per message.
const CHATS = /^\/workspaces\/[^/]+\/chats(\/unread-count)?$/

const push = coalescer(
  (paths) => {
    const filter = (m: AnyRouteMatch) => touches(loadedBy(m), paths)
    router?.clearCache({ filter }) // hover-preloaded data for these paths is now stale
    if (router?.state.matches.some(filter)) void reload(router, filter, touches(APP_PATHS, paths))
    for (const s of subscribers) if (touches(s.paths, paths)) s.refetch()
  },
  { throttle: (p) => (LIST.test(p) ? 3000 : CHATS.test(p) ? 1000 : undefined) },
)

const join = (ch: Channel) => send({ type: 'collab.join', workspace: ch.workspace, [ch.kind]: ch.key })
// Collab replies name the note for a note session, else the issue.
const channel = (msg: { issue?: string; note?: string }) =>
  [...channels].find((c) => (msg.note ? c.kind === 'note' && c.key === msg.note : c.kind === 'issue' && c.key === msg.issue))

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
    const name = extractDataFromArray(extractDataFromArray(app, 'workspaces', []).find((w) => w.slug === workspace), 'name', workspace)
    notice = `You no longer have access to ${name}.`
    workspace = undefined
    void homePath().then((href) => router?.navigate({ href }))
    return
  }
  push(paths)
}

function onMessage(msg: Message) {
  switch (msg.type) {
    case 'resync':
      push(ALL)
      break
    case 'event':
      onEvent(msg)
      break
    case 'collab.joined': {
      const ch = channel(msg)
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
      channel(msg)?.onClose()
      break
    case 'presence':
      channel({ issue: msg.issue })?.onPresence(msg.viewers)
      break
    case 'typing':
      for (const fn of typists) fn(msg)
      break
  }
}

const socket = createSocket({
  binary: true,
  onHello(id, again, reauthed) {
    client = id
    setClient(id)
    if (workspace) send({ type: 'subscribe', workspace })
    for (const ch of channels) join(ch)
    if (again) setTimeout(() => push(ALL), reauthed ? 0 : Math.random() * 2000) // spread a restart's refetches over 2s
  },
  onDrop() {
    client = ''
    setClient('')
    for (const ch of channels) {
      ch.uuid = ch.id = null
      ch.onClose()
      ch.onPresence([])
    }
  },
  onMessage(data) {
    if (!(data instanceof ArrayBuffer)) return onMessage(data as Message)
    const frame = decodeFrame(new Uint8Array(data))
    const ch = frame && [...channels].find((c) => c.uuid === frame.id)
    if (ch) ch.onFrame(frame.type, frame.payload)
  },
  async stillSignedIn() {
    if ((await refreshAccess().catch(() => null))?.status !== 401) return true
    sessionEnded()
    return false
  },
})
// Only after `hello`; everything sent before then is sent again on hello.
const send = socket.send

function start(r: AnyRouter) {
  router = r
  const stop = socket.start()
  const unsubscribe = r.subscribe('onResolved', () => loaded.prune(r.state.matches.map(keyOf)))
  return () => {
    unsubscribe()
    stop()
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

// Reloads the given routes' matches (e.g. the chat list and the open chat after this tab's own change), keeping the session.
export const reloadRoutes = (r: AnyRouter, ids: string[]) => reload(r, (m) => ids.includes(m.routeId), false)

// Tells the chat's other viewers this user is typing (in a thread when `thread` is its root's id). Callers throttle.
export const sendTyping = (chat: string, thread: string | null) => send({ type: 'typing', chat, thread })

// Others typing in any chat; returns the unsubscribe.
export function onTyping(fn: (t: Typing) => void) {
  typists.add(fn)
  return () => void typists.delete(fn)
}

// Live editing of one issue's description or note's body over the shared socket. onOpen fires on every (re)join,
// followed by the stored updates and `ready`; onClose when the socket drops. Re-joins by itself on reconnect.
export function joinCollab(workspace: string, kind: CollabKind, key: string, handlers: Handlers) {
  const ch: Channel = { ...handlers, workspace, kind, key, uuid: null, id: null }
  channels.add(ch)
  join(ch)
  return {
    send(type: number, payload: Uint8Array) {
      return ch.id ? socket.sendFrame(encodeFrame(type, ch.id, payload)) : false
    },
    leave() {
      channels.delete(ch)
      if (ch.uuid) send({ type: 'collab.leave', issue: ch.uuid })
    },
  }
}

// Refetches `paths` in this tab as if another tab had changed them: for this tab's own writes that other parts of the
// page show too (a manual mark updates the run, the history and the cases' last results).
export const refetchPaths = (paths: string[]) => push(paths)
