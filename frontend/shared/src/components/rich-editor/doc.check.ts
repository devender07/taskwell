// Run: node --experimental-strip-types shared/src/components/rich-editor/doc.check.ts
import { assert } from '../../check.ts'
import { imageSrc, measure } from './doc.ts'

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
const file = '/api/files/0f8fad5b-d9cb-469f-a165-70867728950e/shot.png'
assert.equal(imageSrc(file), file)
for (const bad of ['https://attacker.test/px.png', '//attacker.test/px', 'javascript:alert(1)', 'data:image/png;base64,AA', `${file}?x=1`, `${file}/x`, '/api/files/x/a.png', ` ${file}`, null, 1])
  assert.ok(imageSrc(bad) === null, `kept ${String(bad)}`)
console.log('doc: ok')
