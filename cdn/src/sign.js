// File links signed by the user API (backend/user/internal/file): base64url HMAC-SHA256 of "<path>\n<exp>".

const enc = new TextEncoder()

const b64url = (buf) => btoa(String.fromCharCode(...new Uint8Array(buf))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')

function fromB64url(s) {
  const bin = atob(s.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - (s.length % 4)) % 4))
  return Uint8Array.from(bin, (c) => c.charCodeAt(0))
}

const hmacKey = (secret, usage) => crypto.subtle.importKey('raw', enc.encode(secret), { name: 'HMAC', hash: 'SHA-256' }, false, [usage])

export async function sign(secret, path, exp) {
  return b64url(await crypto.subtle.sign('HMAC', await hmacKey(secret, 'sign'), enc.encode(`${path}\n${exp}`)))
}

// Whether sig signs path and exp, and exp (Unix seconds) is after now. The comparison is constant time.
export async function verify(secret, path, exp, sig, now) {
  if (!/^\d{1,12}$/.test(exp ?? '') || Number(exp) <= now || !/^[A-Za-z0-9_-]{43}$/.test(sig ?? '')) return false
  return crypto.subtle.verify('HMAC', await hmacKey(secret, 'verify'), fromB64url(sig), enc.encode(`${path}\n${exp}`))
}
