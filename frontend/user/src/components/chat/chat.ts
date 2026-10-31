// Pure helpers for the chat view, checked by chat.check.ts.

type Msg = { id: string; author: { id: string } | null; created_at: string; deleted: boolean }

const DAY = 86_400_000
const COLLAPSE_MS = 5 * 60_000

const dayStart = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()

// "Today", "Yesterday", "Saturday, October 31" (with the year when it isn't this year), in local time.
export function dayLabel(iso: string, now = new Date(), locale?: string) {
  const d = new Date(iso)
  const days = Math.round((dayStart(now) - dayStart(d)) / DAY)
  if (days === 0) return 'Today'
  if (days === 1) return 'Yesterday'
  const year = d.getFullYear() === now.getFullYear() ? undefined : 'numeric'
  return d.toLocaleDateString(locale, { weekday: 'long', month: 'long', day: 'numeric', year })
}

export type Row<M> = { msg: M; day: string | null; compact: boolean }

// Messages (oldest first) as rendered rows: a day divider before the first message of each day, and `compact` (no
// avatar or name) for a message by the same author as the one before it within 5 minutes on the same day.
export function groupMessages<M extends Msg>(messages: M[], now = new Date(), locale?: string): Row<M>[] {
  let prev: M | undefined
  return messages.map((msg) => {
    const t = Date.parse(msg.created_at)
    const newDay = !prev || dayStart(new Date(prev.created_at)) !== dayStart(new Date(t))
    const compact =
      !!prev && !newDay && !msg.deleted && !prev.deleted && !!msg.author && msg.author.id === prev.author?.id && t - Date.parse(prev.created_at) < COLLAPSE_MS
    prev = msg
    return { msg, day: newDay ? dayLabel(msg.created_at, now, locale) : null, compact }
  })
}

// The id of the first unread message: walking back from the newest, the `unread`-th one not by `me`.
// ponytail: the chat's unread count also counts thread replies, so with unread replies the divider sits a little high.
export function firstUnread(messages: Msg[], unread: number, me: string) {
  if (unread <= 0) return null
  let left = unread
  for (let i = messages.length - 1; i >= 0; i--) {
    if (messages[i]!.author?.id === me) continue
    if (--left === 0) return messages[i]!.id
  }
  return messages[0]?.id ?? null
}

// "Maya is typing…", "Maya and Ravi are typing…", "Several people are typing…".
export function typingText(names: string[]) {
  if (names.length === 0) return ''
  if (names.length === 1) return `${names[0]} is typing…`
  if (names.length === 2) return `${names[0]} and ${names[1]} are typing…`
  return 'Several people are typing…'
}

export const firstName = (name: string) => name.split(/\s+/)[0] || name
