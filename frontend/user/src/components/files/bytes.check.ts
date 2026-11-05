// Run: node --experimental-strip-types user/src/components/files/bytes.check.ts
import { assert } from '../../../../shared/src/check.ts'
import { formatBytes } from './bytes.ts'

assert.equal(formatBytes(0), '0 B')
assert.equal(formatBytes(1023), '1023 B')
assert.equal(formatBytes(1536), '1.5 KB')
assert.equal(formatBytes(1.2 * 1024 ** 3), '1.2 GB')
assert.equal(formatBytes(5120 * 1024 ** 2), '5 GB')
assert.equal(formatBytes(15.4 * 1024 ** 2), '15 MB')
console.log('ok')
