// Run: node --experimental-strip-types shared/src/time/relative.check.ts
import { assert } from '../check.ts'
import { relativeTime } from './relative.ts'

const now = new Date(2026, 9, 31, 12, 0, 0)
const at = (y: number, m: number, d: number, h = 0, min = 0, s = 0) => new Date(y, m, d, h, min, s).toISOString()

assert.equal(relativeTime(at(2026, 9, 31, 11, 59, 30), now), 'just now')
assert.equal(relativeTime(at(2026, 9, 31, 12, 0, 30), now), 'just now')
assert.equal(relativeTime(at(2026, 9, 31, 11, 55), now), '5m ago')
assert.equal(relativeTime(at(2026, 9, 31, 9, 0), now), '3h ago')
assert.equal(relativeTime(at(2026, 9, 30, 12, 30), now), '23h ago')
assert.equal(relativeTime(at(2026, 9, 30, 8, 0), now), 'yesterday')
assert.equal(relativeTime(at(2026, 9, 30, 0, 0), now), 'yesterday')
// Dates follow the locale; pinned here so the check passes on any machine.
assert.equal(relativeTime(at(2026, 9, 29, 23, 59), now, 'en-US'), 'Oct 29')
assert.equal(relativeTime(at(2026, 9, 12), now, 'en-US'), 'Oct 12')
assert.equal(relativeTime(at(2025, 9, 12), now, 'en-US'), 'Oct 12, 2025')
assert.equal(relativeTime(at(2025, 9, 12), now, 'de-DE'), '12. Okt. 2025')
assert.equal(relativeTime(at(2026, 9, 12), now), new Date(2026, 9, 12).toLocaleDateString(undefined, { month: 'short', day: 'numeric' }))
console.log('relative ok')
