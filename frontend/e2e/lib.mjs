// Shared setup for the e2e scripts: playwright-core and Chrome come from the machine, not from npm (see README.md).
import { createHmac } from 'node:crypto'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const PLAYWRIGHT_CORE =
  process.env.PLAYWRIGHT_CORE ?? '/home/devender/.nvm/versions/node/v22.14.0/lib/node_modules/@playwright/cli/node_modules/playwright-core/index.mjs'
const { chromium } = await import(PLAYWRIGHT_CORE)

export const USER_URL = process.env.E2E_USER_URL ?? 'http://localhost:5173'
export const ADMIN_URL = process.env.E2E_ADMIN_URL ?? 'http://localhost:5174'
export const WORKSPACE = process.env.E2E_WORKSPACE ?? 'acme'
export const PROJECT = process.env.E2E_PROJECT ?? 'ENG'
// The seeded demo accounts (backend seed); override for another environment.
export const DEMO = { email: process.env.E2E_EMAIL ?? 'demo@tether.test', name: 'Demo' }
export const MAYA = { email: process.env.E2E_EMAIL_2 ?? 'maya@tether.test', name: 'Maya' }
export const PASSWORD = process.env.E2E_PASSWORD ?? 'Demo12345'

export const launch = () => chromium.launch({ executablePath: process.env.CHROME_PATH ?? '/usr/bin/google-chrome', headless: !process.env.HEADED })

// Where failure screenshots go.
export const shot = (name) => join(process.env.E2E_OUT ?? tmpdir(), name)

export const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

// Polls `fn` until it returns truthy (errors count as not yet), for up to `ms`.
export async function waitFor(fn, ms = 4000) {
  const end = Date.now() + ms
  while (Date.now() < end) {
    try {
      if (await fn()) return true
    } catch {}
    await sleep(100)
  }
  return false
}

// PASS/FAIL/SKIP lines and a summary; the process exits non-zero if anything failed.
export function reporter() {
  let passed = 0
  let failed = 0
  let skipped = 0
  return {
    ok(name, pass, extra = '') {
      if (pass) passed++
      else failed++
      console.log(`${pass ? 'PASS' : 'FAIL'} ${name}${extra ? ` (${extra})` : ''}`)
    },
    skip(name, why) {
      skipped++
      console.log(`SKIP ${name} (${why})`)
    },
    done() {
      console.log(`\n${passed} passed, ${failed} failed, ${skipped} skipped`)
      if (failed) process.exitCode = 1
    },
  }
}

// RFC 6238 code (SHA-1, 30 s, 6 digits) for a base32 secret; `step` shifts the time window.
export function totp(secret, step = 0) {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
  const bits = [...secret.replace(/=+$/, '').toUpperCase()].map((c) => alphabet.indexOf(c).toString(2).padStart(5, '0')).join('')
  const key = Buffer.from(bits.match(/.{8}/g).map((b) => parseInt(b, 2)))
  const counter = Buffer.alloc(8)
  counter.writeBigInt64BE(BigInt(Math.floor(Date.now() / 30_000) + step))
  const h = createHmac('sha1', key).update(counter).digest()
  const o = h[19] & 15
  return String((h.readUInt32BE(o) & 0x7fffffff) % 1_000_000).padStart(6, '0')
}

// A 1×1 pink PNG, for upload tests.
export const PNG = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP4LyHxHwAFhgJ2bM4p2QAAAABJRU5ErkJggg==', 'base64')
