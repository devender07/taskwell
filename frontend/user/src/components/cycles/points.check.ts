// Run: node --experimental-strip-types user/src/components/cycles/points.check.ts
import { assert } from '../../../../shared/src/check.ts'
import type { Cycle } from '../../api.ts'
import { burndownSeries, velocity } from './points.ts'

const cycle = (number: number, ends_on: string, total: number, completed: number, state: Cycle['state'] = 'past') =>
  ({ number, ends_on, state, progress: { points: { total, started: 0, completed } } }) as Cycle

assert.equal(velocity([]), null)
assert.equal(velocity([cycle(1, '2026-01-14', 0, 0), cycle(2, '2026-01-28', 10, 4, 'current')]), null)

const v = velocity([
  cycle(1, '2026-01-14', 10, 2),
  cycle(5, '2026-03-11', 10, 9),
  cycle(2, '2026-01-28', 10, 4),
  cycle(4, '2026-02-25', 10, 8),
  cycle(3, '2026-02-11', 10, 6),
  cycle(6, '2026-03-25', 20, 20, 'current'),
])!
assert.equal(v.average, 8) // (6 + 8 + 9) / 3, rounded
assert.deepEqual(v.shown.map((c) => c.number), [1, 2, 3, 4, 5])
assert.equal(velocity([cycle(1, '2026-01-14', 5, 3)])!.average, 3)

const range = { starts_on: '2026-03-02', ends_on: '2026-03-15' }
const s = burndownSeries(range, { planned: 20, days: [{ date: '2026-03-02', scope: 20, done: 0 }, { date: '2026-03-05', scope: 24, done: 6 }] })
assert.equal(s.last, 13)
assert.deepEqual(s.remaining.map((r) => [r.x, r.y]), [[0, 20], [3, 18]])
assert.equal(s.top, 20)
assert.equal(s.empty, false)
assert.equal(burndownSeries(range, { planned: null, days: [] }).empty, true)
assert.equal(burndownSeries(range, { planned: 0, days: [{ date: '2026-03-02', scope: 0, done: 0 }] }).top, 1)
assert.equal(burndownSeries({ starts_on: '2026-03-02', ends_on: '2026-03-02' }, { planned: 3, days: [] }).last, 1)
