import type { ReactNode } from 'react'

export type Option = { value: string; label: string; hint?: string; disabled?: boolean; icon?: ReactNode }

// Case-insensitive ranking: label starts with the query, then a word in the label does, then the label
// contains it, then the hint does. Stable within each rank. An empty query keeps the original order.
// ponytail: linear scan per keystroke; fine for a few thousand options, use loadOptions beyond that.
export function filterOptions(options: Option[], query: string, limit = 50): Option[] {
  const q = query.trim().toLowerCase()
  if (!q) return options.slice(0, limit)
  const rank = (o: Option) => {
    const label = o.label.toLowerCase()
    if (label.startsWith(q)) return 0
    if (label.split(/[\s\-_.@]+/).some((w) => w.startsWith(q))) return 1
    if (label.includes(q)) return 2
    if (o.hint?.toLowerCase().includes(q)) return 3
    return -1
  }
  return options
    .map((o) => ({ o, r: rank(o) }))
    .filter((x) => x.r >= 0)
    .sort((a, b) => a.r - b.r)
    .slice(0, limit)
    .map((x) => x.o)
}

// [before, match, after] for the first case-insensitive occurrence of query in text, or null.
export function splitMatch(text: string, query: string): [string, string, string] | null {
  const q = query.trim()
  const i = q ? text.toLowerCase().indexOf(q.toLowerCase()) : -1
  return i < 0 ? null : [text.slice(0, i), text.slice(i, i + q.length), text.slice(i + q.length)]
}
