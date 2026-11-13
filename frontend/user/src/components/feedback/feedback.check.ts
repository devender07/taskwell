// Run: node --experimental-strip-types user/src/components/feedback/feedback.check.ts
import { assert } from '../../../../shared/src/check.ts'
import { feedbackPrefill, feedbackWho } from './feedback.ts'

const p = feedbackPrefill({ id: 'f1', body: '\n  Export to CSV please  \n\nWe need it weekly.' })
assert.equal(p.title, 'Export to CSV please')
assert.equal(p.feedback, 'f1')
assert.deepEqual(p.description.content?.map((n) => n.content?.[0]?.text ?? ''), ['Export to CSV please  ', '', 'We need it weekly.'])
assert.equal(feedbackPrefill({ id: 'f2', body: 'x'.repeat(100) }).title.length, 80)

assert.equal(feedbackWho({ name: 'Ana', email: 'ana@acme.com', created_by: null }), 'Ana')
assert.equal(feedbackWho({ name: null, email: 'ana@acme.com', created_by: null }), 'ana@acme.com')
assert.equal(feedbackWho({ name: null, email: null, created_by: { id: 'u', name: 'Dev' } }), 'Added by Dev')
console.log('feedback: ok')
