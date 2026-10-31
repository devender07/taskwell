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

// Uploaded files are the only images and the only relative links a document may hold.
export const FILE_URL = /^\/api\/files\/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\/[^/?#]+$/i

// An image's src as rendered: dropped unless it is an uploaded file, whether the doc came from JSON, a paste or Yjs.
export const imageSrc = (src: unknown) => (typeof src === 'string' && FILE_URL.test(src) ? src : null)
