// Run: node --experimental-strip-types user/src/components/chat/emoji.check.ts
import { assert } from '../../../../shared/src/check.ts'
import { EMOJI, searchEmoji } from './emoji.ts'

assert.equal(new Set(EMOJI.map((e) => e.emoji)).size, EMOJI.length)
assert.equal(EMOJI.every((e) => e.emoji && e.words), true)
assert.equal(searchEmoji('').length, EMOJI.length)
assert.equal(searchEmoji('Thumbs UP')[0]?.emoji, '👍')
assert.equal(searchEmoji('heart').some((e) => e.emoji === '💔'), true)
assert.equal(searchEmoji('zzzz').length, 0)
