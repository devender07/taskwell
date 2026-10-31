// Admin app: sign-in with password + TOTP, the realtime socket, sign-out. Needs the admin's TOTP secret (README.md).
import { ADMIN_URL as A, DEMO, PASSWORD, launch, reporter, shot, sleep, totp, waitFor } from './lib.mjs'

const t = reporter()
const secret = process.env.E2E_ADMIN_TOTP_SECRET
if (!secret) {
  t.skip('admin', 'set E2E_ADMIN_TOTP_SECRET to the admin account’s TOTP setup key')
  t.done()
  process.exit()
}

const errors = []
const browser = await launch()
const page = await (await browser.newContext({ viewport: { width: 1280, height: 860 } })).newPage()
page.on('pageerror', (e) => errors.push(e.message))
const sockets = []
page.on('websocket', (ws) => sockets.push(ws.url()))

try {
  await page.goto(`${A}/users`)
  await page.waitForURL(/\/signin\?redirect=/)
  t.ok('admin: signed out goes to sign-in with a redirect', decodeURIComponent(page.url()).includes('redirect=/users'))
  await page.getByLabel('Email').fill(DEMO.email)
  await page.getByLabel('Password').fill(PASSWORD)
  await page.getByRole('button', { name: 'Sign in' }).click()
  await page.getByLabel('Code').waitFor()
  t.ok('admin: password asks for an authenticator code', true)

  await page.getByLabel('Code').fill('000000')
  await page.getByRole('button', { name: 'Continue' }).click()
  t.ok('admin: a wrong code is refused', await waitFor(() => page.getByText('That code is incorrect.').isVisible()))

  // A code works once: if this window's was used (a recent run), wait for the next one.
  const accepted = async () => {
    await page.getByLabel('Code').fill(totp(secret))
    await page.getByRole('button', { name: 'Continue' }).click()
    return waitFor(() => new URL(page.url()).pathname === '/users', 3000)
  }
  let signedIn = await accepted()
  if (!signedIn) {
    await sleep(30_000 - (Date.now() % 30_000) + 500)
    signedIn = await accepted()
  }
  t.ok('admin: TOTP code signs in and returns to the redirect', signedIn, page.url())
  await page.locator('h1').waitFor()
  t.ok('admin: realtime socket connects', await waitFor(() => sockets.some((u) => u.endsWith('/api/realtime'))))

  await page.getByRole('button', { name: 'Sign out' }).click()
  t.ok('admin: sign-out returns to sign-in', await waitFor(() => new URL(page.url()).pathname === '/signin'))
} catch (e) {
  t.ok('run', false, e.message.split('\n')[0])
  await page.screenshot({ path: shot('e2e-fail-admin.png') }).catch(() => {})
} finally {
  await browser.close()
  t.ok('no page errors', errors.length === 0, errors.join('; '))
  t.done()
}
