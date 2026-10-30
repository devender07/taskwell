// Run: node --experimental-strip-types shared/src/components/date-picker/calendar.check.ts
import { assert } from '../../check.ts'
import { addDays, addMonths, monthGrid, parseISO, sameMonth } from './calendar.ts'

assert.equal(parseISO('2026-02-30'), null)
assert.equal(parseISO(''), null)
assert.equal(parseISO('2026-11-02')?.getDate(), 2)

assert.equal(addDays('2026-12-31', 1), '2027-01-01')
assert.equal(addDays('2026-03-01', -1), '2026-02-28')
assert.equal(addMonths('2026-01-31', 1), '2026-02-28')
assert.equal(addMonths('2024-01-31', 1), '2024-02-29')
assert.equal(addMonths('2026-01-15', -1), '2025-12-15')

// November 2026 starts on a Sunday: the grid starts Monday Oct 26, 42 days, contiguous.
const g = monthGrid('2026-11-17')
assert.equal(g.length, 42)
assert.equal(g[0], '2026-10-26')
assert.equal(g[6], '2026-11-01')
assert.equal(g[41], '2026-12-06')
assert.ok(g.every((d, i) => i === 0 || addDays(g[i - 1]!, 1) === d), 'grid days are consecutive')
// A month starting on Monday starts the grid on the 1st.
assert.equal(monthGrid('2026-06-10')[0], '2026-06-01')
assert.ok(sameMonth('2026-11-01', '2026-11-30') && !sameMonth('2026-11-30', '2026-12-01'))

console.log('calendar: ok')
