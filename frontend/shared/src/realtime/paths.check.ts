// Run: node --experimental-strip-types shared/src/realtime/paths.check.ts
import { assert } from '../check.ts'
import { coalescer, decodeFrame, encodeFrame, eventPaths, matchKey, pathMatches, touches, uuidBytes, uuidString } from './paths.ts'

assert.ok(pathMatches('/workspaces/acme/issues/ENG-1', '/workspaces/acme/issues/ENG-1'))
assert.ok(pathMatches('/workspaces/acme/projects?archived=true', '/workspaces/acme/projects'))
assert.ok(!pathMatches('/workspaces/acme/issues/ENG-12', '/workspaces/acme/issues/ENG-1'))
assert.ok(!pathMatches('/workspaces/acme', '/workspaces/acme/'))
assert.ok(pathMatches('/workspaces/acme/issues/ENG-1/timeline', '/workspaces/acme/'))
assert.ok(!pathMatches('/workspaces/acme-2/issues', '/workspaces/acme/'))
assert.ok(pathMatches('/workspaces/acme/issues/', '/workspaces/acme/issues/ENG-1'))
assert.ok(pathMatches('/me', '/'))
assert.ok(touches(new Set(['/a', '/b?x=1']), ['/c', '/b']))
assert.ok(!touches([], ['/']))

const e = { type: 'event' as const, kind: 'issue.updated', origin: 'me', paths: ['/a'] }
assert.deepEqual(eventPaths(e, 'me'), [])
assert.deepEqual(eventPaths(e, 'other'), ['/a'])
assert.deepEqual(eventPaths({ ...e, origin: '' }, ''), ['/a'])

assert.equal(matchKey('/_app/$workspace', '/$workspace', { workspace: 'acme', identifier: 'ENG-1' }, ''), matchKey('/_app/$workspace', '/$workspace', { workspace: 'acme', identifier: 'ENG-2' }, ''))
assert.ok(matchKey('/i', '/$workspace/issue/$identifier', { workspace: 'acme', identifier: 'ENG-1' }, '') !== matchKey('/i', '/$workspace/issue/$identifier', { workspace: 'acme', identifier: 'ENG-2' }, ''))
assert.ok(matchKey('/p', '/$workspace/projects', { workspace: 'acme' }, { archived: true }) !== matchKey('/p', '/$workspace/projects', { workspace: 'acme' }, {}))

// Fake clock: timers run when time advances past them.
let now = 0
let timers: { at: number; fn: () => void }[] = []
const clock = { now: () => now, after: (fn: () => void, ms: number) => timers.push({ at: now + ms, fn }) }
const advance = (ms: number) => {
  now += ms
  const due = timers.filter((t) => t.at <= now)
  timers = timers.filter((t) => t.at > now)
  for (const t of due) t.fn()
}
const flushed: string[][] = []
const push = coalescer((p) => flushed.push(p), { clock, throttle: (p) => (p === '/stats' ? 5000 : undefined) })

push(['/a', '/b'])
push(['/b', '/c'])
advance(100)
assert.equal(flushed.length, 0)
advance(50)
assert.deepEqual(flushed, [['/a', '/b', '/c']])
push([])
advance(200)
assert.equal(flushed.length, 1)

push(['/stats', '/users'])
advance(150)
assert.deepEqual(flushed[1], ['/stats', '/users'])
push(['/stats', '/users'])
advance(150)
assert.deepEqual(flushed[2], ['/users'])
push(['/stats'])
advance(150)
assert.equal(flushed.length, 3)
advance(5000 - 300 - 1)
assert.equal(flushed.length, 3)
advance(1)
assert.deepEqual(flushed[3], ['/stats'])

const id = '0f8fad5b-d9cb-469f-a165-70867728950e'
assert.equal(uuidString(uuidBytes(id)), id)
const frame = encodeFrame(2, uuidBytes(id), new Uint8Array([7, 8]))
assert.equal(frame.length, 19)
const decoded = decodeFrame(frame)!
assert.equal(decoded.type, 2)
assert.equal(decoded.id, id)
assert.deepEqual([...decoded.payload], [7, 8])
assert.equal(decodeFrame(new Uint8Array(5)), null)
console.log('realtime paths ok')
