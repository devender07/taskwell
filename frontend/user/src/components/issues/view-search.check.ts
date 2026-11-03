// Run: node --experimental-strip-types user/src/components/issues/view-search.check.ts
import { assert } from '../../../../shared/src/check.ts'
import type { Issue, Status } from '../../api.ts'
import { hasFilters, issuePageQuery, orderGroups, validateViewSearch, type IssuePage } from './view-search.ts'

const id = (n: number) => `00000000-0000-0000-0000-${String(n).padStart(12, '0')}`
const status = (n: number, position: number): Status => ({ id: id(n), name: `S${n}`, category: 'unstarted', position, icon: null, color: null })
const [todo, done] = [status(1, 1), status(2, 0)]
const me = id(10)
const other = id(11)

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

// Page query: board never ungrouped, My issues never by assignee and without assignee/cycle filters.
assert.equal(issuePageQuery({}), 'limit=50&group=status&sort=priority')
assert.equal(issuePageQuery({ view: 'board', group: 'none', sort: 'due', priority: 1 }), 'limit=50&group=status&sort=due&priority=1')
assert.equal(issuePageQuery({ group: 'assignee', assignee: 'me', cycle: 'none', label: id(50) }, true), `limit=50&group=status&sort=priority&label=${id(50)}`)
assert.equal(issuePageQuery({ group: 'none', assignee: 'me,none' }), 'limit=50&group=none&sort=priority&assignee=me%2Cnone')
assert.ok(hasFilters({ assignee: 'me' }) && !hasFilters({ assignee: 'me' }, true) && !hasFilters({ view: 'board' }))

// Grouping order over a server page; empty groups filled in, assignees named from their issues.
const ctx = { statuses: [todo, done], userId: me }
const page = (groups: [string, Issue[]][]): IssuePage => ({
  total: groups.reduce((n, [, l]) => n + l.length, 0),
  groups: groups.map(([key, list]) => ({ key, count: list.length, issues: list, has_more: false })),
})
const stranger = id(12)
assert.deepEqual(orderGroups(page([[todo.id, [issues[0]!, issues[2]!]]]), 'status', ctx).map((g) => [g.key, g.count, nums(g.issues)]), [
  [done.id, 0, []],
  [todo.id, 2, [1, 3]],
])
assert.equal(orderGroups(page([[todo.id, issues]]), 'status', ctx)[1]!.status, todo)
assert.deepEqual(orderGroups(page([['3', [issues[1]!]]]), 'priority', ctx).map((g) => [g.label, g.count]), [
  ['Urgent', 0], ['High', 0], ['Medium', 1], ['Low', 0], ['No priority', 0],
])
const byAssignee = page([['none', [issues[1]!]], [stranger, [issue(5, { assignee: { id: stranger, name: 'Bob' } })]], [me, [issues[0]!]]])
assert.deepEqual(orderGroups(byAssignee, 'assignee', ctx).map((g) => [g.label, g.key === 'none' ? 'none' : '', g.count]), [
  ['Zed', '', 1], ['Bob', '', 1], ['No assignee', 'none', 1],
])
assert.deepEqual(orderGroups(page([['all', issues]]), 'none', ctx).map((g) => [g.key, g.count]), [['all', 4]])
console.log('ok')
