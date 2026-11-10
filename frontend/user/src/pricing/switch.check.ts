// Run: node --experimental-strip-types user/src/pricing/switch.check.ts
import { assert } from '../../../shared/src/check.ts'
import { switchLabel, switchSummary } from './switch.ts'
import { TIERS, type Tier } from './tiers.ts'

const [starter, pro, advanced] = TIERS as [Tier, Tier, Tier]
assert.equal(switchLabel('starter', pro, 'month'), 'Upgrade to Pro')
assert.equal(switchLabel('advanced', starter, 'year'), 'Downgrade to Starter')
assert.equal(switchLabel('pro', pro, 'year'), 'Switch to yearly')
assert.equal(switchLabel('pro', pro, 'month'), 'Switch to monthly')
assert.equal(switchLabel('pro', advanced, 'year'), 'Upgrade to Advanced')

const base = { currency: 'USD', recurring: '10000', next_billed_at: '2026-11-15T04:15:20Z' }
const trial = switchSummary({ ...base, trialing: true, today: null }, 'year')
assert.ok(trial[0].startsWith('Your free trial continues until') && trial[1].includes('100') && trial[1].endsWith('/ year.'))
const charge = switchSummary({ ...base, trialing: false, today: { action: 'charge', amount: '450' } }, 'month')
assert.ok(charge[0].startsWith("You'll pay") && charge[0].includes('4.50') && charge[1].includes(' from '))
const credit = switchSummary({ ...base, trialing: false, today: { action: 'credit', amount: '450' }, next_billed_at: null }, 'month')
assert.ok(credit[0].includes('in credit') && credit[1].endsWith('/ month.'))
assert.equal(switchSummary({ ...base, trialing: false, today: null }, 'month')[0], 'Nothing to pay today.')
console.log('ok')
