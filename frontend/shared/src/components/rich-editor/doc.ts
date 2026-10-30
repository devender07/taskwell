import type { JSONContent } from '@tiptap/react'

// Text length the API counts (text + mention labels, UTF-16 units), and whether the doc is empty (no text,
// mentions, rules or images), in which case it is stored as null.
export function measure(doc: JSONContent): { length: number; empty: boolean } {
  let length = 0
  let solid = false
  const walk = (n: JSONContent) => {
    if (n.type === 'mention') {
      length += String(n.attrs?.label ?? '').length
      solid = true
    }
    if (n.type === 'horizontalRule' || n.type === 'image') solid = true
    length += n.text?.length ?? 0
    n.content?.forEach(walk)
  }
  walk(doc)
  return { length, empty: length === 0 && !solid }
}
