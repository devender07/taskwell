import { assert } from '../check.ts'
import { byId, extractDataFromArray, idSet, isEmpty, isUndefined } from './list.ts'

const rows = [{ id: 'a', n: 1 }, { id: 'b', n: 2 }]
assert.equal(byId(rows, 'b')?.n, 2)
assert.equal(byId(rows, 'z'), undefined)
assert.equal(byId(rows, null), undefined)
assert.equal(byId(undefined, 'a'), undefined)
assert.deepEqual([...idSet(rows)], ['a', 'b'])
const run: { results?: { id: string; title?: string }[]; name: string; owner?: { name: string } | null } = { name: 'x' }
assert.deepEqual(extractDataFromArray(run, 'results', []), [])
assert.deepEqual(extractDataFromArray(null as typeof run | null, 'results', []), [])
assert.deepEqual(extractDataFromArray({ results: 'oops' } as unknown as typeof run, 'results', []), [])
assert.deepEqual(extractDataFromArray({ ...run, results: [{ id: 'a' }] }, 'results', []), [{ id: 'a' }])
assert.equal(extractDataFromArray(run, 'owner.name'), '')
assert.equal(extractDataFromArray({ ...run, owner: null }, 'owner.name', 'Nobody'), 'Nobody')
assert.equal(extractDataFromArray({ ...run, owner: { name: 'Ann' } }, 'owner.name', 'Nobody'), 'Ann')
assert.equal(extractDataFromArray({ results: [{ id: 'a', title: 'T' }] }, 'results.0.title'), 'T')
assert.equal(extractDataFromArray({ results: [{ id: 'a' }] }, ['results', 0, 'id']), 'a')
assert.equal(extractDataFromArray({ results: [] }, 'results.5.id', 'none'), 'none')
assert.ok(isUndefined(null) && isUndefined(undefined) && isUndefined('undefined') && !isUndefined(0) && !isUndefined(''))
assert.ok(isEmpty('  ') && isEmpty([]) && isEmpty({}) && isEmpty(null) && !isEmpty(0) && !isEmpty('a') && !isEmpty([1]))
console.log('list ok')
