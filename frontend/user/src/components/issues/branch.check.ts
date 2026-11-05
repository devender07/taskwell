// Run: node --experimental-strip-types user/src/components/issues/branch.check.ts
import { assert } from '../../../../shared/src/check.ts'
import { branchName, commitMessage } from './branch.ts'

assert.equal(branchName('ENG-142', 'Fix login redirect'), 'eng-142-fix-login-redirect')
assert.equal(branchName('ENG-1', '  Crème brûlée: 2× faster!! '), 'eng-1-creme-brulee-2-faster')
assert.equal(branchName('ENG-7', '日本語'), 'eng-7')
assert.equal(branchName('ENG-7', ''), 'eng-7')
assert.equal(branchName('ENG-142', 'Make the onboarding checklist remember dismissed steps'), 'eng-142-make-the-onboarding-checklist-remember-dis')
// The 50th character is a dash: it's dropped, not kept as a trailing "-".
assert.equal(branchName('ENG-1', 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa bbb'), 'eng-1-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')
assert.equal(commitMessage('ENG-142', ' Fix login redirect '), 'ENG-142 Fix login redirect')
assert.equal(commitMessage('ENG-7', ''), 'ENG-7')
