import type { JSONContent } from '@tiptap/react'
import { API_URL, apiUrl } from '../../api-url/api-url.ts'

export type Doc = JSONContent

// Text length the API counts (text + mention labels, UTF-16 units), and whether the doc is empty (no text,
// mentions, rules or images), in which case it is stored as null.
export function measure(doc: Doc): { length: number; empty: boolean } {
  let length = 0
  let solid = false
  const walk = (n: Doc) => {
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

// An image's src as rendered, on the API's host: dropped unless it is an uploaded file, whether the doc came from
// JSON, a paste or Yjs.
export const imageSrc = (src: unknown) => (typeof src === 'string' && FILE_URL.test(src) ? apiUrl(src) : null)

// The stored path of a rendered file URL, so copying an image within the editor keeps it.
export const filePath = (url: string) => (API_URL && url.startsWith(API_URL + '/') ? url.slice(API_URL.length) : url)

const SAFE_HREF = /^(https?:\/\/|mailto:)/i

// Links may point at the web, email or an uploaded file; anything else (javascript:, data:, relative paths) is not a link.
export const isAllowedHref = (url: string) => SAFE_HREF.test(url) || FILE_URL.test(url)
export const linkHref = (href: unknown) => (typeof href === 'string' && isAllowedHref(href) ? apiUrl(href) : undefined)
