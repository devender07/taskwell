// Run: node --experimental-strip-types user/src/redirect.check.ts
import { assert } from '../../shared/src/check.ts'
import { safeRedirect } from './api.ts'

assert.equal(safeRedirect('/invite?token=abc'), '/invite?token=abc')
assert.equal(safeRedirect('/'), '/')
assert.equal(safeRedirect('//evil.com'), undefined)
assert.equal(safeRedirect('/\\evil.com'), undefined)
assert.equal(safeRedirect('https://evil.com'), undefined)
assert.equal(safeRedirect('invite'), undefined)
assert.equal(safeRedirect(undefined), undefined)
console.log('ok')
