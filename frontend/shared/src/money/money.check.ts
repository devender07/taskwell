// Run: node --experimental-strip-types shared/src/money/money.check.ts
import { assert } from '../check.ts'
import { money } from './money.ts'

const digits = (s: string) => s.replace(/[^\d.,]/g, '')
assert.equal(digits(money(1250, 'USD')), new Intl.NumberFormat().format(12.5) + '0')
assert.equal(digits(money(1200, 'USD')), '12')
assert.equal(digits(money(500, 'JPY')), '500') // no minor unit
console.log('ok')
