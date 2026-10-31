import { useRouter, type AnyRouteMatch, type AnyRouter } from '@tanstack/react-router'
import { useEffect } from 'react'
import { coalescer, eventPaths, matchKey, touches, type RealtimeEvent } from '@tether/shared/realtime'
import { api, getMe, setClient } from '@/api'

// The admin service's realtime socket (docs/realtime-plan.md): events name the admin API paths that changed and
// the tab reloads the route matches that loaded them.

type Message = RealtimeEvent | { type: 'hello'; client: string } | { type: 'pong' | 'resync' | 'error' }
type LoaderContext = { route: { id: string; fullPath: string }; params: Record<string, string>; deps: unknown }

// ponytail: one entry per route match ever loaded, never pruned; prune on navigation if long sessions grow it.
const loaded = new Map<string, Set<string>>()

let router: AnyRouter | null = null
let ws: WebSocket | null = null
let client = ''
let running = false
let opened = false
let delay = 1000
let retry: ReturnType<typeof setTimeout> | undefined
let ping: ReturnType<typeof setInterval> | undefined
let pong: ReturnType<typeof setTimeout> | undefined

// A loader's GET, tracked so events for its path reload the match.
export function loaderApi<T>(ctx: LoaderContext, path: string) {
  const key = matchKey(ctx.route.id, ctx.route.fullPath, ctx.params, ctx.deps)
  let paths = loaded.get(key)
  if (!paths) loaded.set(key, (paths = new Set()))
  paths.add(path)
  return api<T>(path)
}

const push = coalescer(
  (paths) => {
    const filter = (m: AnyRouteMatch) => touches(loaded.get(matchKey(m.routeId, m.fullPath, m.params, m.loaderDeps)) ?? [], paths)
    if (router?.state.matches.some(filter)) void router.invalidate({ filter })
  },
  { throttle: { '/stats': 5000 } },
)

// Only after `hello`.
const send = (msg: object) => {
  if (client && ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify(msg))
}

function onMessage(data: unknown) {
  let msg: Message
  try {
    msg = JSON.parse(String(data))
  } catch {
    return
  }
  if (msg.type === 'hello') {
    client = msg.client
    setClient(client)
    delay = 1000
    if (opened) push(['/'])
    opened = true
    clearInterval(ping)
    ping = setInterval(checkAlive, 25_000)
  } else if (msg.type === 'pong') clearTimeout(pong)
  else if (msg.type === 'resync') push(['/'])
  else if (msg.type === 'event') push(eventPaths(msg, client))
}

function checkAlive() {
  if (ws?.readyState !== WebSocket.OPEN) return
  send({ type: 'ping' })
  clearTimeout(pong)
  pong = setTimeout(() => {
    const sock = ws
    if (!sock) return
    sock.onclose = null
    sock.close()
    dropped(sock, true)
  }, 10_000)
}

function connect() {
  const sock = new WebSocket(`${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/api/realtime`)
  ws = sock
  sock.onmessage = (e) => onMessage(e.data)
  sock.onclose = () => dropped(sock, client !== '')
}

// A connect that fails before `hello` may be a lapsed session: back to sign-in if so.
function dropped(sock: WebSocket, hello: boolean) {
  if (ws !== sock) return
  ws = null
  client = ''
  setClient('')
  clearInterval(ping)
  clearTimeout(pong)
  if (!running) return
  retry = setTimeout(async () => {
    if (!hello && !(await getMe().catch(() => true))) {
      location.assign(`/signin?redirect=${encodeURIComponent(location.pathname + location.search)}`)
      return
    }
    if (running && !ws) connect()
  }, delay)
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

// Mounted once by the _app layout.
export function useRealtime() {
  const r = useRouter()
  useEffect(() => start(r), [r])
}
