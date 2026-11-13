// Run: node --experimental-strip-types user/src/components/templates/template.check.ts
import { assert } from '../../../../shared/src/check.ts'
import type { Template } from '../../api.ts'
import { fillEmpty, forProjectFirst, repeatSummary } from './template.ts'

const t = {
  id: 't1',
  name: 'Bug',
  title: 'Weekly report',
  description: { type: 'doc' },
  project: { id: 'p', key: 'ENG', name: 'Engineering' },
  status: { id: 's2' },
  assignee: { id: 'u1', name: 'Ann' },
  priority: 2,
  labels: [{ id: 'l1' }],
  repeat: { every: 2, unit: 'week', next_on: '2026-11-10', paused: false },
} as unknown as Template
const fmt = (d: string) => `<${d}>`

assert.equal(repeatSummary(t, fmt), 'Every 2 weeks · next <2026-11-10>')
assert.equal(repeatSummary({ repeat: { every: 1, unit: 'day', next_on: '2026-11-10', paused: false } }, fmt), 'Every day · next <2026-11-10>')
assert.equal(repeatSummary({ repeat: { every: 1, unit: 'day', next_on: '2026-11-10', paused: true } }, fmt), 'Paused')
assert.equal(repeatSummary({ repeat: null }, fmt), null)

const empty = { title: '', description: null, statusId: 's1', priority: '0', assignee: null, labelIds: [] }
assert.deepEqual(fillEmpty(t, empty, false), { title: 'Weekly report', description: { type: 'doc' }, statusId: 's2', priority: '2', assignee: { id: 'u1', name: 'Ann' }, labelIds: ['l1'] })
const typed = { title: 'Mine', description: { type: 'doc', content: [] }, statusId: 's3', priority: '4', assignee: { id: 'u2', name: 'Bo' }, labelIds: ['l9'] }
assert.deepEqual(fillEmpty(t, typed, true), typed)
assert.equal(fillEmpty(t, empty, true).statusId, 's1')
assert.equal(fillEmpty({ ...t, title: '', status: null, priority: 0 }, empty, false).title, '')

const other = { ...t, id: 't2', project: null }
assert.deepEqual(forProjectFirst([other, t], 'ENG').map((x) => x.id), ['t1', 't2'])
assert.deepEqual(forProjectFirst([t, other]).map((x) => x.id), ['t1', 't2'])
