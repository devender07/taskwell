// Run: node --experimental-strip-types user/src/components/home/home.check.ts
import { assert } from '../../../../shared/src/check.ts'
import type { Issue, StatusCategory } from '../../api.ts'
import { greeting, localDate, pushRecent, splitMine } from './home.ts'

const now = new Date(2026, 9, 31, 9)
const day = (n: number) => localDate(new Date(2026, 9, 31 + n))
const issue = (number: number, due: string | null, priority = 0, category: StatusCategory = 'unstarted') =>
  ({ number, identifier: `ENG-${number}`, due_date: due, priority, status: { category } }) as Issue

const { attention, rest } = splitMine(
  [issue(1, day(3), 2), issue(2, day(-1)), issue(3, day(3), 1), issue(4, day(8)), issue(5, null, 1), issue(6, day(0), 0, 'completed'), issue(7, day(7))],
  now,
)
assert.deepEqual(attention.map((i) => i.number), [2, 3, 1, 7])
assert.deepEqual(rest.map((i) => i.number), [5, 4])

assert.equal(greeting(new Date(2026, 0, 1, 4)), 'Good evening')
assert.equal(greeting(new Date(2026, 0, 1, 5)), 'Good morning')
assert.equal(greeting(new Date(2026, 0, 1, 12)), 'Good afternoon')
assert.equal(greeting(new Date(2026, 0, 1, 17)), 'Good evening')

let list = Array.from({ length: 20 }, (_, i) => ({ identifier: `ENG-${i}`, title: '' }))
list = pushRecent(list, { identifier: 'ENG-5', title: 'new' })
assert.equal(list.length, 20)
assert.deepEqual(list[0], { identifier: 'ENG-5', title: 'new' })
assert.equal(list.filter((r) => r.identifier === 'ENG-5').length, 1)
list = pushRecent(list, { identifier: 'ENG-99', title: '' })
assert.equal(list.length, 20)
assert.equal(list.at(-1)!.identifier, 'ENG-18')

console.log('home: ok')
