// Run: node --experimental-strip-types user/src/components/chat/chat.check.ts
import { assert } from '../../../../shared/src/check.ts'
import { dayLabel, firstUnread, groupMessages, typingText } from './chat.ts'

const now = new Date(2026, 9, 31, 15)
const at = (day: number, h: number, m = 0) => new Date(2026, 9, day, h, m).toISOString()
const msg = (id: string, author: string | null, created_at: string, deleted = false) => ({ id, author: author ? { id: author } : null, created_at, deleted })

assert.equal(dayLabel(at(31, 0, 1), now, 'en-US'), 'Today')
assert.equal(dayLabel(at(30, 23, 59), now, 'en-US'), 'Yesterday')
assert.equal(dayLabel(at(24, 12), now, 'en-US'), 'Saturday, October 24')
assert.equal(dayLabel(new Date(2025, 9, 31, 12).toISOString(), now, 'en-US'), 'Friday, October 31, 2025')

const rows = groupMessages(
  [
    msg('a', 'maya', at(30, 9)),
    msg('b', 'maya', at(30, 9, 4)), // same author within 5 min: compact
    msg('c', 'maya', at(30, 9, 10)), // 6 min later: new group
    msg('d', 'demo', at(30, 9, 11)),
    msg('e', 'demo', at(31, 0, 1)), // new day: divider, not compact
    msg('f', 'demo', at(31, 0, 2), true), // deleted: never compact
    msg('g', 'demo', at(31, 0, 3)), // after a deleted one: not compact
    msg('h', null, at(31, 0, 3)),
    msg('i', null, at(31, 0, 3)), // unknown authors don't collapse
  ],
  now,
  'en-US',
)
assert.deepEqual(rows.map((r) => r.day), ['Yesterday', null, null, null, 'Today', null, null, null, null])
assert.deepEqual(rows.map((r) => r.compact), [false, true, false, false, false, false, false, false, false])

const list = [msg('1', 'maya', at(31, 1)), msg('2', 'demo', at(31, 2)), msg('3', 'maya', at(31, 3)), msg('4', 'demo', at(31, 4))]
assert.equal(firstUnread(list, 0, 'demo'), null)
assert.equal(firstUnread(list, 1, 'demo'), '3')
assert.equal(firstUnread(list, 2, 'demo'), '1')
assert.equal(firstUnread(list, 9, 'demo'), '1')

assert.equal(typingText([]), '')
assert.equal(typingText(['Maya']), 'Maya is typing…')
assert.equal(typingText(['Maya', 'Ravi']), 'Maya and Ravi are typing…')
assert.equal(typingText(['Maya', 'Ravi', 'Ana']), 'Several people are typing…')

console.log('chat: ok')
