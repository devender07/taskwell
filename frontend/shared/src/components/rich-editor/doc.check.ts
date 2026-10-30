// Run: node --experimental-strip-types shared/src/components/rich-editor/doc.check.ts
import { assert } from '../../check.ts'
import { measure } from './doc.ts'

assert.deepEqual(measure({ type: 'doc', content: [{ type: 'paragraph' }] }), { length: 0, empty: true })
assert.deepEqual(measure({ type: 'doc', content: [{ type: 'horizontalRule' }] }), { length: 0, empty: false })
assert.deepEqual(measure({ type: 'doc', content: [{ type: 'image', attrs: { src: '/api/files/x/a.png' } }] }), { length: 0, empty: false })
assert.deepEqual(
  measure({
    type: 'doc',
    content: [{ type: 'paragraph', content: [{ type: 'text', text: 'Ping 😀 ' }, { type: 'mention', attrs: { id: 'u1', label: 'Maya' } }] }],
  }),
  { length: 12, empty: false },
)
console.log('doc: ok')
