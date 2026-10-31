export type RealtimeEvent = { type: 'event'; kind: string; workspace?: string; origin?: string; paths: string[]; new_slug?: string }

// A loaded path matches a changed one when they are equal, ignoring the query; either side ending in "/" is a prefix.
export function pathMatches(loaded: string, changed: string) {
  const path = loaded.split('?')[0]!
  return path === changed || (changed.endsWith('/') && path.startsWith(changed)) || (path.endsWith('/') && changed.startsWith(path))
}

export const touches = (loaded: Iterable<string>, changed: string[]) => [...loaded].some((l) => changed.some((c) => pathMatches(l, c)))

// The paths an event asks this tab to refetch: none when the tab made the change itself.
export const eventPaths = (e: RealtimeEvent, self: string) => (e.origin && e.origin === self ? [] : e.paths)

// Identifies a route match by its route and the params and deps its loader saw. Only the params in the route's own
// path count, so a parent's key doesn't change when a child's param does.
export function matchKey(routeId: string, fullPath: string, params: Record<string, string>, deps: unknown) {
  const path = fullPath.replace(/\$(\w+)/g, (_, k: string) => encodeURIComponent(params[k] ?? ''))
  return `${routeId} ${path} ${JSON.stringify(deps ?? '')}`
}

type Clock = { now: () => number; after: (fn: () => void, ms: number) => unknown }
const realClock: Clock = { now: () => Date.now(), after: (fn, ms) => setTimeout(fn, ms) }

// Collects paths for `wait` ms and flushes them de-duplicated. A path `throttle` gives ms for flushes at most once per
// that gap; later requests for it wait for the gap and go out together.
export function coalescer(
  flush: (paths: string[]) => void,
  { wait = 150, throttle = (_: string): number | undefined => undefined, clock = realClock } = {},
) {
  let pending = new Set<string>()
  let scheduled = false
  const last = new Map<string, number>()

  const run = () => {
    scheduled = false
    const now = clock.now()
    const out: string[] = []
    const later = new Set<string>()
    let next = Infinity
    for (const p of pending) {
      const gap = throttle(p)
      const since = now - (last.get(p) ?? -Infinity)
      if (gap && since < gap) {
        later.add(p)
        next = Math.min(next, gap - since)
      } else {
        out.push(p)
        if (gap) last.set(p, now)
      }
    }
    pending = later
    if (later.size) schedule(next)
    if (out.length) flush(out)
  }
  const schedule = (ms: number) => {
    if (scheduled) return
    scheduled = true
    clock.after(run, ms)
  }

  return (paths: string[]) => {
    for (const p of paths) pending.add(p)
    if (pending.size) schedule(wait)
  }
}

// Collab frames on the shared socket: type byte, the issue uuid as 16 bytes, then the payload.
export function uuidBytes(uuid: string) {
  const hex = uuid.replace(/-/g, '')
  return Uint8Array.from({ length: 16 }, (_, i) => parseInt(hex.slice(i * 2, i * 2 + 2), 16))
}

export const uuidString = (b: Uint8Array) => {
  const hex = [...b].map((x) => x.toString(16).padStart(2, '0')).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}

export function encodeFrame(type: number, id: Uint8Array, payload: Uint8Array) {
  const frame = new Uint8Array(17 + payload.length)
  frame[0] = type
  frame.set(id, 1)
  frame.set(payload, 17)
  return frame
}

export const decodeFrame = (data: Uint8Array) =>
  data.length < 17 ? null : { type: data[0]!, id: uuidString(data.subarray(1, 17)), payload: data.subarray(17) }
