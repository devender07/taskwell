// Run: node --experimental-strip-types user/src/components/analytics/analytics.check.ts
import { assert } from '../../../../shared/src/check.ts'
import type { TeamMember } from '../../api.ts'
import { barPercent, days, sortTeam, validateTeamSearch, weekLabel } from './analytics.ts'

assert.equal(barPercent(0, 10), 0)
assert.equal(barPercent(5, 0), 0)
assert.equal(barPercent(10, 10), 100)
assert.equal(barPercent(3, 12), 25)
assert.equal(barPercent(1, 1000), 4)

assert.equal(weekLabel('2026-08-17'), 'Aug 17')
assert.equal(weekLabel('2026-01-05'), 'Jan 5')
assert.equal(days(1), '1 day')
assert.equal(days(2.5), '2.5 days')
assert.equal(days(0), 'under a day')

assert.deepEqual(validateTeamSearch({ sort: 'open', desc: true }), { sort: 'open', desc: true })
assert.deepEqual(validateTeamSearch({ sort: 'name', desc: 'yes' }), {})
assert.deepEqual(validateTeamSearch({ sort: 'nope' }), {})

const row = (name: string, open: number, cycle: number | null) =>
  ({ id: name, name, open, overdue: 0, completed_30d: 0, cycle_time_days: cycle }) as TeamMember
const rows = [row('Cara', 2, 3), row('Abe', 5, null), row('Bo', 2, 1)]
assert.deepEqual(sortTeam(rows, {}).map((r) => r.name), ['Abe', 'Bo', 'Cara'])
assert.deepEqual(sortTeam(rows, { desc: true }).map((r) => r.name), ['Cara', 'Bo', 'Abe'])
assert.deepEqual(sortTeam(rows, { sort: 'open' }).map((r) => r.name), ['Bo', 'Cara', 'Abe'])
assert.deepEqual(sortTeam(rows, { sort: 'open', desc: true }).map((r) => r.name), ['Abe', 'Bo', 'Cara'])
assert.deepEqual(sortTeam(rows, { sort: 'cycle_time_days' }).map((r) => r.name), ['Bo', 'Cara', 'Abe'])
assert.deepEqual(sortTeam(rows, { sort: 'cycle_time_days', desc: true }).map((r) => r.name), ['Cara', 'Bo', 'Abe'])
