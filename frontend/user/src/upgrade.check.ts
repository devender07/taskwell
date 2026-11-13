// Run: node --experimental-strip-types user/src/upgrade.check.ts
import { assert } from '../../shared/src/check.ts'
import { upgradesFor } from './api.ts'

const plans = { notes: ['pro', 'advanced'], chat: ['starter', 'pro', 'advanced'], files: ['free'], roadmap: [] }
assert.deepEqual(upgradesFor({ notes: false, chat: false, files: false, roadmap: false }, plans, 'free'), { notes: 'pro', chat: 'starter' })
assert.deepEqual(upgradesFor({ notes: false, chat: true }, plans, 'starter'), { notes: 'pro' })
assert.deepEqual(upgradesFor({ notes: false }, plans, 'advanced'), {})
assert.deepEqual(upgradesFor({ notes: false }, null, 'free'), {})
assert.deepEqual(upgradesFor({ notes: false }, plans, undefined), {})
console.log('ok')
