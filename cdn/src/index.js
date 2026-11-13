// Serves uploads at /<file id>/<name>?exp=&sig= on links the user API signs after its access check, so this Worker
// never calls the API. Objects never change (a new upload is a new id), so the edge keeps each one for a year under a
// key shared by every link and user; the browser keeps it until its link expires.
import { verify } from './sign.js'

const PATH = /^\/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\/([^/]+)$/
// Shown inline; anything else downloads. Matches the user API's media types.
const MEDIA = new Set(['image/png', 'image/jpeg', 'image/gif', 'image/webp', 'video/mp4', 'video/webm'])
const EDGE_CACHE = 'public, max-age=31536000, immutable'

const empty = (status, headers = {}) => new Response(null, { status, headers: { 'Cache-Control': 'no-store', ...headers } })

// RFC 5987 filename*: the name re-encoded so only attr-chars stay literal; null when the path's escaping is invalid.
function filename(escaped) {
  try {
    return encodeURIComponent(decodeURIComponent(escaped)).replace(/['()*]/g, (c) => '%' + c.charCodeAt(0).toString(16).toUpperCase())
  } catch {
    return null
  }
}

function objectHeaders(obj) {
  const h = new Headers()
  h.set('Content-Type', obj.httpMetadata?.contentType || 'application/octet-stream')
  h.set('ETag', obj.httpEtag)
  h.set('Accept-Ranges', 'bytes')
  return h
}

// The object from the edge cache, or else from R2 (filling the cache with the whole object). Range requests (video)
// are answered from either. null when the object doesn't exist.
async function load(request, env, ctx, origin, id) {
  const key = `${origin}/${id}`
  const range = request.headers.get('Range')
  const hit = await caches.default.match(new Request(key, { headers: range ? { Range: range } : {} }))
  if (hit) return hit

  const obj = await env.BUCKET.get(id, range ? { range: request.headers } : undefined)
  if (!obj) return null
  // Stores the whole object at the edge: body is a copy of obj's when obj is whole, else it's read again from R2.
  const fill = async (body) => {
    const whole = body ? obj : await env.BUCKET.get(id)
    if (!whole) return
    const h = objectHeaders(whole)
    h.set('Content-Length', String(whole.size))
    h.set('Cache-Control', EDGE_CACHE)
    await caches.default.put(key, new Response(body ?? whole.body, { headers: h }))
  }
  const h = objectHeaders(obj)
  if (!range) {
    const [mine, copy] = obj.body.tee()
    ctx.waitUntil(fill(copy).catch(() => {}))
    h.set('Content-Length', String(obj.size))
    return new Response(mine, { headers: h })
  }
  ctx.waitUntil(fill().catch(() => {}))
  const r = obj.range ?? {}
  const offset = 'suffix' in r ? obj.size - r.suffix : (r.offset ?? 0)
  const length = 'suffix' in r ? r.suffix : (r.length ?? obj.size - offset)
  h.set('Content-Range', `bytes ${offset}-${offset + length - 1}/${obj.size}`)
  h.set('Content-Length', String(length))
  return new Response(obj.body, { status: 206, headers: h })
}

export default {
  async fetch(request, env, ctx) {
    if (request.method !== 'GET' && request.method !== 'HEAD') return empty(405, { Allow: 'GET, HEAD' })
    const url = new URL(request.url)
    const m = PATH.exec(url.pathname)
    const name = m && filename(m[2])
    if (!name) return empty(404)
    const now = Math.floor(Date.now() / 1000)
    const exp = url.searchParams.get('exp')
    if (!env.FILES_SECRET || !(await verify(env.FILES_SECRET, url.pathname, exp, url.searchParams.get('sig'), now))) return empty(403)

    const res = await load(request, env, ctx, url.origin, m[1])
    if (!res) return empty(404)
    const h = new Headers(res.headers)
    h.set('Content-Disposition', MEDIA.has(h.get('Content-Type')) ? 'inline' : `attachment; filename*=UTF-8''${name}`)
    h.set('Cache-Control', `private, max-age=${Number(exp) - now}`)
    h.set('X-Content-Type-Options', 'nosniff')
    h.set('Content-Security-Policy', "default-src 'none'; sandbox")
    h.set('Cross-Origin-Resource-Policy', 'cross-origin')
    return new Response(request.method === 'HEAD' ? null : res.body, { status: res.status, headers: h })
  },
}
