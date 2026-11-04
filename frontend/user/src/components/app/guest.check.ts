// Run: node --experimental-strip-types user/src/components/app/guest.check.ts
import { assert } from '../../../../shared/src/check.ts'
import { guestBlocked } from './guest.ts'

assert.equal(guestBlocked('/acme'), false)
assert.equal(guestBlocked('/acme/projects/ABC'), false)
assert.equal(guestBlocked('/acme/issue/ABC-1'), false)
assert.equal(guestBlocked('/acme/inbox/c/123'), true)
assert.equal(guestBlocked('/acme/roadmap'), true)
assert.equal(guestBlocked('/acme/members/123'), true)
assert.equal(guestBlocked('/acme/settings'), false)
assert.equal(guestBlocked('/acme/settings/profile'), false)
assert.equal(guestBlocked('/acme/settings/api-tokens'), false)
assert.equal(guestBlocked('/acme/settings/workspace'), true)
assert.equal(guestBlocked('/acme/settings/roles/new'), true)
console.log('ok')
