// node worker.check.mjs: the signing test vector shared with the user API's Go test, and the Worker against an
// in-memory R2 bucket and edge cache.
import assert from 'node:assert/strict'
import worker from './src/index.js'
import { sign, verify } from './src/sign.js'

const ID = '0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b'
const PATH = `/${ID}/photo%201.png`
const now = Math.floor(Date.now() / 1000)

assert.equal(await sign('test-secret', PATH, 1767312000), '7EzqQ3f5boRpA14Ux8-zYIfFyL4IfxF2bb2GA55QiUQ')
const exp = String(now + 3600)
const sig = await sign('test-secret', PATH, exp)
assert.ok(await verify('test-secret', PATH, exp, sig, now))
assert.ok(!(await verify('test-secret', `/${ID}/other.png`, exp, sig, now)), 'changed path')
assert.ok(!(await verify('test-secret', PATH, String(now + 7200), sig, now)), 'changed exp')
assert.ok(!(await verify('test-secret', PATH, exp, sig, now + 3600)), 'expired')
assert.ok(!(await verify('other', PATH, exp, sig, now)), 'other secret')
assert.ok(!(await verify('test-secret', PATH, exp, undefined, now)), 'no sig')

// Fakes: R2 objects and the edge cache, enough for the Worker's calls.
const bytes = new TextEncoder().encode('0123456789')
const objects = { [ID]: { type: 'image/png', bytes } }
let gets = 0
const env = {
  FILES_SECRET: 'test-secret',
  BUCKET: {
    async get(id, opts) {
      gets++
      const o = objects[id]
      if (!o) return null
      let body = o.bytes, range
      const h = opts?.range?.get?.('Range')
      if (h) {
        const [a, b] = h.replace('bytes=', '').split('-').map(Number)
        range = { offset: a, length: b - a + 1 }
        body = o.bytes.slice(a, b + 1)
      }
      return { size: o.bytes.length, httpEtag: '"e1"', httpMetadata: { contentType: o.type }, range, body: new Response(body).body }
    },
  },
}
const store = new Map()
globalThis.caches = {
  default: {
    async match(req) {
      const r = store.get(req.url)
      return r ? r.clone() : undefined
    },
    async put(key, res) {
      store.set(key, new Response(await res.arrayBuffer(), { headers: res.headers }))
    },
  },
}
const waits = []
const ctx = { waitUntil: (p) => waits.push(p) }
const get = (path, headers) => worker.fetch(new Request(`https://files.test${path}`, { headers }), env, ctx)

let res = await get(`${PATH}?exp=${exp}&sig=${sig}`)
assert.equal(res.status, 200)
assert.equal(await res.text(), '0123456789')
assert.equal(res.headers.get('Content-Type'), 'image/png')
assert.equal(res.headers.get('Content-Disposition'), 'inline')
assert.match(res.headers.get('Cache-Control'), /^private, max-age=(3600|3599)$/)
assert.equal(res.headers.get('X-Content-Type-Options'), 'nosniff')
await Promise.all(waits)
assert.equal(store.size, 1, 'filled the edge cache')
assert.equal(store.get(`https://files.test/${ID}`).headers.get('Cache-Control'), 'public, max-age=31536000, immutable')

gets = 0
res = await get(`${PATH}?exp=${exp}&sig=${sig}`)
assert.equal(await res.text(), '0123456789')
assert.equal(gets, 0, 'served from the edge cache')

store.clear()
res = await get(`${PATH}?exp=${exp}&sig=${sig}`, { Range: 'bytes=2-4' })
assert.equal(res.status, 206)
assert.equal(await res.text(), '234')
assert.equal(res.headers.get('Content-Range'), 'bytes 2-4/10')

objects[ID].type = 'application/pdf'
store.clear()
res = await get(`${PATH}?exp=${exp}&sig=${sig}`)
assert.equal(res.headers.get('Content-Disposition'), "attachment; filename*=UTF-8''photo%201.png")

assert.equal((await get(`${PATH}?exp=${exp}&sig=${sig[0] === "A" ? "B" : "A"}${sig.slice(1)}`)).status, 403)
assert.equal((await get(`${PATH}?exp=${exp}`)).status, 403)
assert.equal((await get(`/not-an-id/x.png?exp=${exp}&sig=${sig}`)).status, 404)
const missing = `/${'1'.repeat(8)}-1111-1111-1111-${'1'.repeat(12)}/x.png`
assert.equal((await get(`${missing}?exp=${exp}&sig=${await sign('test-secret', missing, exp)}`)).status, 404)
assert.equal((await worker.fetch(new Request(`https://files.test${PATH}`, { method: 'PUT', body: 'x' }), env, ctx)).status, 405)
console.log('worker checks passed')
