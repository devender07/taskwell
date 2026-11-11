// Run: node --experimental-strip-types user/src/pricing/switch.check.ts
import { assert } from '../../../shared/src/check.ts'
import { switchLabel, switchSummary } from './switch.ts'
import { limitLines, TIERS, type Tier } from './tiers.ts'

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
assert.deepEqual(limitLines({ id: 'free', workspaces: 1, projects: 1, members: 1, storage_gb: 2, ai_usd: 0 }), ['1 workspace', '1 project', 'Just you, no other members', '2 GB storage'])
assert.deepEqual(limitLines({ id: 'pro', workspaces: 3, projects: 0, members: 25, storage_gb: 100, ai_usd: 3 }), [
  '3 workspaces',
  'Unlimited projects',
  'Up to 25 members per workspace',
  '100 GB storage per workspace',
  '$3 of AI test runs a month',
])
assert.deepEqual(limitLines({ id: 'advanced', workspaces: 0, projects: 0, members: 0, storage_gb: 500, ai_usd: 15 }).slice(0, 4), [
  'Unlimited workspaces',
  'Unlimited projects',
  'Unlimited members',
  '500 GB storage per workspace',
])
console.log('ok')
