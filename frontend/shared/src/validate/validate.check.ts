// Run: node --experimental-strip-types shared/src/validate/validate.check.ts
import assert from 'node:assert/strict'
import { email, name, password, required, username, usernameOrEmail } from './validate.ts'

assert.equal(email(' a@b.co '), null)
assert.ok(email(''))
assert.ok(email('a@b'))
assert.ok(email('a b@c.com'))
assert.equal(username('dev_07.x'), null)
assert.ok(username('ab'))
assert.ok(username('_dev'))
assert.ok(username('dev ender'))
assert.equal(name('Ada Lovelace'), null)
assert.ok(name('   '))
assert.ok(name('x'.repeat(101)))
assert.equal(password('correct horse'), null)
assert.ok(password('short'))
assert.ok(password('é'.repeat(40)))
assert.equal(usernameOrEmail('dev07'), null)
assert.ok(usernameOrEmail('dev@'))
assert.ok(required('Code')(' '))
console.log('validate: ok')
