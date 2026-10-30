// Run: node --experimental-strip-types shared/src/components/smart-autocomplete/match.check.ts
import { assert } from '../../check.ts'
import { filterOptions, splitMatch } from './match.ts'

const opts = [
  { value: '1', label: 'Bob Stone', hint: 'bob@acme.com' },
  { value: '2', label: 'Alice Bobson' },
  { value: '3', label: 'Jacob', hint: 'jbob@acme.com' },
  { value: '4', label: 'Carol', hint: 'carol@bob.dev' },
  { value: '5', label: 'Dave' },
]
const labels = (q: string) => filterOptions(opts, q).map((o) => o.label)

// Starts-with, then word-start, then contains, then hint; non-matches dropped.
assert.deepEqual(labels('bob'), ['Bob Stone', 'Alice Bobson', 'Jacob', 'Carol'])
assert.deepEqual(labels('  BOB '), labels('bob'))
assert.deepEqual(labels('ston'), ['Bob Stone'])
assert.deepEqual(labels('zzz'), [])
// Empty query keeps order; limit applies.
assert.equal(labels('').length, 5)
assert.equal(filterOptions(opts, '', 2).length, 2)

assert.deepEqual(splitMatch('Alice Bobson', 'bob'), ['Alice ', 'Bob', 'son'])
assert.equal(splitMatch('Dave', 'bob'), null)
assert.equal(splitMatch('Dave', ''), null)

console.log('match: ok')
