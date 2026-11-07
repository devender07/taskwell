// Run: node --experimental-strip-types shared/src/money/money.check.ts
import { assert } from '../check.ts'
import { formatMoney } from './money.ts'

assert.equal(formatMoney(2900, 'usd', 'en-US'), '$29')
assert.equal(formatMoney(149900, 'inr', 'en-IN'), '₹1,499')
assert.equal(formatMoney(2950, 'usd', 'en-US'), '$29.50')
assert.equal(formatMoney(0, 'usd', 'en-US'), '$0')
assert.equal(formatMoney(500, 'jpy', 'en-US'), '¥500')
console.log('ok')
