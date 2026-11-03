// Run: node --experimental-strip-types user/src/components/roadmap/timeline.check.ts
import { assert } from '../../../../shared/src/check.ts'
import { fromDay, months, percentDone, ticks, timeline, toDay, widthOf, xOf } from './timeline.ts'

assert.equal(fromDay(toDay('2026-03-08')), '2026-03-08') // across a DST change
assert.equal(toDay('2026-03-01') - toDay('2026-02-28'), 1)

// No dates: the zoom's span around today, padded to whole months.
const q = timeline([], '2026-11-02', 'quarter')
assert.equal(fromDay(q.start), '2026-08-01') // today - 46 days = Sep 17, a month before is Aug
assert.equal(fromDay(q.start + q.days), '2027-02-01') // today + 46 = Dec 18, end of Jan
assert.equal(q.dayPx, 8)

// Dates outside the span widen it, a month each side.
const wide = timeline(['2026-05-15', '2027-04-30'], '2026-11-02', 'month')
assert.equal(fromDay(wide.start), '2026-04-01')
assert.equal(fromDay(wide.start + wide.days), '2027-06-01')

assert.equal(xOf(q, '2026-08-01'), 0)
assert.equal(xOf(q, '2026-08-03'), 16)
assert.equal(widthOf(q, '2026-08-01', '2026-08-01'), 8) // a one-day range is one day wide

const m = months(q)
assert.deepEqual(m.map((x) => x.label), ['Aug 2026', 'Sep', 'Oct', 'Nov', 'Dec', 'Jan 2027'])
assert.equal(m[1]!.x, 31 * 8)

const mondays = ticks(q, 'quarter')
assert.equal(mondays[0]!.label, '3') // Aug 3 2026 is the first Monday
assert.ok(mondays.every((t, i) => i === 0 || t.x - mondays[i - 1]!.x === 56), 'weekly ticks')
assert.equal(ticks(q, 'month').length, q.days)
assert.equal(ticks(q, 'year').length, 6)

assert.equal(percentDone({ total: 40, completed: 12, canceled: 2 }), 32)
assert.equal(percentDone({ total: 2, completed: 0, canceled: 2 }), 0)
assert.equal(percentDone({ total: 0, completed: 0, canceled: 0 }), 0)
console.log('timeline: ok')
