// Run: node --experimental-strip-types user/src/components/settings/audit.check.ts
import { assert } from '../../../../shared/src/check.ts'
import { auditAction, auditDetail } from './audit.ts'

assert.equal(auditDetail('member.role_changed', { from: 'Member', to: 'Admin' }), 'Member → Admin')
assert.equal(auditDetail('role.updated', { added: ['a', 'b'], removed: ['c'] }), 'Added 2 permissions, removed 1')
assert.equal(auditDetail('role.updated', { removed: ['c'] }), 'Removed 1 permission')
assert.equal(auditDetail('role.updated', { renamed_from: 'Dev', added: [], removed: [] }), 'Renamed from Dev')
assert.equal(auditDetail('role.created', { permissions: ['a'] }), '1 permission')
assert.equal(auditDetail('trash.emptied', { count: 5 }), '5 issues')
assert.equal(auditDetail('member.invited', { role: 'member', guest: false }), 'Member')
assert.equal(auditDetail('member.invited', { role: 'Guest', guest: true }), 'Guest')
assert.equal(auditDetail('webhook.created', {}), '')
assert.equal(auditDetail('billing.plan_changed', { from: 'starter', to: 'pro' }), 'Starter → Pro')
assert.equal(auditDetail('webhook.updated', { from: 'https://a.test', to: 'https://b.test', active: false }), 'https://a.test → https://b.test, Turned off')
assert.equal(auditDetail('test_account.updated', { password_changed: true }), 'Password changed')
assert.equal(auditAction('workspace.require_mfa_changed', { to: true }), 'Required two-factor sign-in')
assert.equal(auditAction('workspace.require_mfa_changed', { to: false }), 'Stopped requiring two-factor sign-in')
assert.equal(auditAction('member.removed', {}), 'Removed a member')
console.log('ok')
