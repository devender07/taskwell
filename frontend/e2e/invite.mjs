// Invite acceptance end to end (README.md): demo invites a fresh address from the members page; the invitee opens the
// emailed link signed out, signs up with the emailed code, accepts and lands in the workspace. Demo then removes them.
import { DEMO, PASSWORD, USER_URL as B, WORKSPACE, launch, reporter, shot, waitFor } from './lib.mjs'

const MAILPIT = process.env.E2E_MAILPIT_URL ?? 'http://localhost:8025'
const API = `${B}/api/workspaces/${WORKSPACE}`
const t = reporter()
const errors = []
const browser = await launch()
const address = `invite-${Date.now()}@taskwell.test`

// The newest email to `to` whose text matches `re`, as the first capture group.
async function mail(to, re) {
  let found = null
  await waitFor(async () => {
    const list = await (await fetch(`${MAILPIT}/api/v1/search?query=${encodeURIComponent(`to:"${to}"`)}`)).json()
    for (const m of list.messages ?? []) {
      const text = (await (await fetch(`${MAILPIT}/api/v1/message/${m.ID}`)).json()).Text ?? ''
      const hit = text.match(re)
      if (hit) return (found = hit[1])
    }
  }, 10_000)
  if (!found) throw new Error(`no email to ${to} matching ${re}`)
  return found
}

const newPage = async (who) => {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 } })
  const page = await ctx.newPage()
  page.on('pageerror', (e) => errors.push(`${who}: ${e.message}`))
  return { ctx, page, req: ctx.request }
}
const demo = await newPage(DEMO.email)
const guest = await newPage(address)
let joined = null

try {
  // One sign-in for demo (over the API) and one sign-up for the guest: auth routes are rate-limited per IP.
  const signin = await demo.req.post(`${B}/api/auth/signin`, { multipart: { email: DEMO.email, password: PASSWORD } })
  if (!signin.ok()) throw new Error(`${DEMO.email} sign-in: HTTP ${signin.status()}`)
  await demo.page.goto(`${B}/${WORKSPACE}/settings/members`)
  await demo.page.getByRole('textbox', { name: 'Email' }).fill(address)
  await demo.page.getByRole('button', { name: 'Send invite' }).click()
  t.ok('invite: sent from the members page', await waitFor(() => demo.page.getByText(`Invite sent to ${address}.`).isVisible(), 5000))

  const link = await mail(address, /(https?:\/\/\S+\/invite\?token=[^\s"<>]+)/)
  t.ok('invite: link emailed', link.startsWith(B), link.replace(/token=.*/, 'token=…'))

  await guest.page.goto(link)
  await guest.page.getByRole('link', { name: 'Create account' }).click()
  await guest.page.getByLabel('Full name').fill('Invite Check')
  await guest.page.getByLabel('Work email').fill(address)
  await guest.page.getByLabel('Password').fill(PASSWORD)
  await guest.page.getByRole('button', { name: 'Create account' }).click()
  await guest.page.getByLabel('Code').waitFor()
  const code = await mail(address, /verification code is (\d{6})/)
  await guest.page.getByLabel('Code').fill(code)
  await guest.page.getByRole('button', { name: 'Verify and continue' }).click()

  const accept = guest.page.getByRole('button', { name: 'Accept invite' })
  t.ok('invite: back on the invite after sign-up', await waitFor(() => accept.isVisible(), 10_000), guest.page.url())
  await accept.click()
  t.ok('invite: accepting lands in the workspace', await waitFor(() => new URL(guest.page.url()).pathname.startsWith(`/${WORKSPACE}`), 10_000), guest.page.url())

  const { members } = await (await demo.req.get(`${API}/members`)).json()
  joined = members.find((m) => m.email === address) ?? null
  t.ok('invite: joined as a member', joined?.role.builtin === 'member' || joined?.role.name === 'Member', joined?.role.name)
} catch (e) {
  t.ok('run', false, e.message.split('\n')[0])
  await demo.page.screenshot({ path: shot('e2e-fail-invite-demo.png') }).catch(() => {})
  await guest.page.screenshot({ path: shot('e2e-fail-invite-guest.png') }).catch(() => {})
} finally {
  // The account itself stays: the API has no way to delete a user. Removing the membership leaves it in no workspace.
  if (joined) t.ok('cleanup: member removed', (await demo.req.delete(`${API}/members/${joined.id}`)).ok())
  else {
    const { invites = [] } = await (await demo.req.get(`${API}/members`)).json().catch(() => ({}))
    for (const i of invites.filter((i) => i.email === address)) await demo.req.delete(`${API}/invites/${i.id}`)
  }
  await browser.close()
  t.ok('no page errors', errors.length === 0, errors.join('; '))
  t.done()
}
