// Run: node --experimental-strip-types user/src/components/issues/view-search.check.ts
import { assert } from '../../../../shared/src/check.ts'
import type { Issue, Member, Status } from '../../api.ts'
import { filterIssues, groupIssues, sortIssues, validateViewSearch } from './view-search.ts'

const id = (n: number) => `00000000-0000-0000-0000-${String(n).padStart(12, '0')}`
const status = (n: number, position: number): Status => ({ id: id(n), name: `S${n}`, category: 'unstarted', position, icon: null, color: null })
const [todo, done] = [status(1, 1), status(2, 0)]
const me = id(10)
const other = id(11)
const members = [
  { id: other, name: 'Alice', email: '', role: { id: '', name: '' } },
  { id: me, name: 'Zed', email: '', role: { id: '', name: '' } },
] as Member[]

const issue = (number: number, o: Partial<Issue>) =>
  ({
    id: id(100 + number),
    number,
    status: todo,
    assignee: null,
    priority: 0,
    due_date: null,
    labels: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...o,
  }) as Issue

const issues = [
  issue(1, { priority: 0, assignee: { id: me, name: 'Zed' }, due_date: '2026-03-01', labels: [{ id: id(50), name: 'Bug', color: '#000000' }] }),
  issue(2, { priority: 3, status: done, updated_at: '2026-01-02T00:00:00.5Z', due_date: '2026-02-01' }),
  issue(3, { priority: 1, assignee: { id: other, name: 'Alice' }, created_at: '2026-01-03T00:00:00Z' }),
  issue(4, { priority: 3, updated_at: '2026-01-02T00:00:00.123Z', cycle: { id: id(60), number: 1, name: 'Cycle 1', starts_on: '2026-01-01', ends_on: '2026-01-14', state: 'past' } }),
]
const nums = (list: Issue[]) => list.map((i) => i.number)

// URL parsing: unknown values dropped, defaults omitted, a lone priority stays numeric.
assert.deepEqual(validateViewSearch({ view: 'list', group: 'status', sort: 'priority' }), {})
assert.deepEqual(validateViewSearch({ view: 'board', group: 'bogus', sort: 'due', priority: 1 }), { view: 'board', sort: 'due', priority: 1 })
assert.deepEqual(validateViewSearch({ priority: '1,9,2,1', assignee: `me,x,none,${other}`, status: 'nope', label: id(50) }), {
  assignee: `me,none,${other}`,
  label: id(50),
  priority: '1,2',
})
assert.deepEqual(validateViewSearch({ cycle: `none,bogus,${id(60)}` }), { cycle: `none,${id(60)}` })

// Filtering: OR within a filter, AND across.
assert.deepEqual(nums(filterIssues(issues, { assignee: 'me,none' }, me)), [1, 2, 4])
assert.deepEqual(nums(filterIssues(issues, { assignee: other }, me)), [3])
assert.deepEqual(nums(filterIssues(issues, { priority: 3, status: done.id }, me)), [2])
assert.deepEqual(nums(filterIssues(issues, { label: id(50) }, me)), [1])
assert.deepEqual(nums(filterIssues(issues, { cycle: id(60) }, me)), [4])
assert.deepEqual(nums(filterIssues(issues, { cycle: 'none' }, me)), [1, 2, 3])
assert.deepEqual(nums(filterIssues(issues, { cycle: `none,${id(60)}` }, me)), [1, 2, 3, 4])

// Sorts, ties by newest number.
assert.deepEqual(nums(sortIssues(issues, 'priority')), [3, 4, 2, 1])
assert.deepEqual(nums(sortIssues(issues, 'updated')), [2, 4, 3, 1])
assert.deepEqual(nums(sortIssues(issues, 'created')), [3, 4, 2, 1])
assert.deepEqual(nums(sortIssues(issues, 'due')), [2, 1, 4, 3])

// Grouping order.
const ctx = { statuses: [todo, done], members, userId: me }
assert.deepEqual(groupIssues(issues, 'status', ctx).map((g) => [g.key, nums(g.issues)]), [[done.id, [2]], [todo.id, [1, 3, 4]]])
assert.deepEqual(groupIssues(issues, 'priority', ctx).map((g) => g.label), ['Urgent', 'High', 'Medium', 'Low', 'No priority'])
assert.deepEqual(groupIssues(issues, 'assignee', ctx).map((g) => [g.label, nums(g.issues)]), [['Zed', [1]], ['Alice', [3]], ['No assignee', [2, 4]]])
assert.equal(groupIssues(issues, 'none', ctx).length, 1)
console.log('ok')
