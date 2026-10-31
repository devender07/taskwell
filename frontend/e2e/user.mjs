// The user app's main flows against a running stack (README.md). Creates one temp issue and deletes it at the end.
import { DEMO, MAYA, PASSWORD, PNG, PROJECT, USER_URL as B, WORKSPACE, launch, reporter, shot, sleep, waitFor } from './lib.mjs'

const API = `${B}/api/workspaces/${WORKSPACE}`
const t = reporter()
const errors = []
const browser = await launch()
const multipart = (o) => ({ multipart: o })

async function newPage(email) {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 } })
  const page = await ctx.newPage()
  page.on('pageerror', (e) => errors.push(`${email}: ${e.message}`))
  return { ctx, page, req: ctx.request }
}

// The DOM a rich-text root renders, as comparable lines, minus ProseMirror's own caret/selection helpers.
const shape = (root) =>
  root.evaluate((el) => {
    const keep = ['href', 'target', 'rel', 'src', 'alt', 'start', 'type', 'data-type', 'data-id', 'data-label', 'class']
    const out = []
    const walk = (n, depth) => {
      for (const c of n.childNodes) {
        if (c.nodeType === 3) {
          if (c.data) out.push(`${'  '.repeat(depth)}"${c.data}"`)
          continue
        }
        if (c.nodeType !== 1 || c.matches('br.ProseMirror-trailingBreak, img.ProseMirror-separator')) continue
        const attrs = keep.filter((a) => c.hasAttribute(a) && !(a === 'class' && /ProseMirror/.test(c.getAttribute(a)))).map((a) => `${a}=${c.getAttribute(a)}`)
        out.push(`${'  '.repeat(depth)}<${c.tagName.toLowerCase()}${attrs.length ? ' ' + attrs.join(' ') : ''}>`)
        walk(c, depth + 1)
      }
    }
    walk(el, 0)
    return out.join('\n')
  })

const demo = await newPage(DEMO.email)
const maya = await newPage(MAYA.email)
let issue = null

try {
  // Sign-in through the form (demo); maya signs in over the API, so one run makes two sign-ins.
  await demo.page.goto(`${B}/signin?method=password`)
  await demo.page.getByLabel('Email').fill(DEMO.email)
  await demo.page.getByLabel('Password').fill(PASSWORD)
  await demo.page.getByRole('button', { name: /sign in/i }).click()
  t.ok('sign-in: password form lands in the workspace', await waitFor(() => new URL(demo.page.url()).pathname.startsWith(`/${WORKSPACE}`), 10_000), demo.page.url())
  const mayaSignin = await maya.req.post(`${B}/api/auth/signin`, multipart({ email: MAYA.email, password: PASSWORD }))
  if (!mayaSignin.ok()) throw new Error(`${MAYA.email} sign-in: HTTP ${mayaSignin.status()}`)

  await maya.page.goto(`${B}/${WORKSPACE}/my-issues`)
  await maya.page.locator('h1').waitFor()
  const bell = maya.page.getByRole('link', { name: /^Inbox/ }).first()
  const bellBefore = await bell.getAttribute('aria-label')

  // Quick-create with a mention and an assignee, from the single-key shortcut.
  await demo.page.goto(`${B}/${WORKSPACE}/projects/${PROJECT}`)
  await demo.page.locator('h1').waitFor()
  await sleep(500)
  await demo.page.keyboard.press('c')
  const dialog = demo.page.getByRole('dialog', { name: 'New issue' })
  const title = `E2E check ${Date.now()}`
  await dialog.getByLabel('Title').fill(title)
  await dialog.locator('[contenteditable=true]').click()
  await demo.page.keyboard.type('hi @Ma')
  await dialog.getByRole('option', { name: /Maya/ }).waitFor()
  await demo.page.keyboard.press('Enter')
  await dialog.getByRole('combobox', { name: 'Assignee' }).fill('Maya')
  await sleep(200)
  await demo.page.keyboard.press('Enter')
  await dialog.getByRole('button', { name: 'Create issue' }).click()
  await demo.page.waitForURL(/\/issue\/[A-Z]+-\d+$/)
  const identifier = demo.page.url().split('/').pop()
  issue = await (await demo.req.get(`${API}/issues/${identifier}`)).json()
  t.ok('quick-create: dialog closes after create', await waitFor(async () => (await demo.page.locator('dialog[open]').count()) === 0))
  t.ok('quick-create: issue created with a mention', JSON.stringify(issue.description).includes('"mention"') && issue.assignee?.name.startsWith('Maya'), identifier)

  t.ok('notifications: maya bell updates live', await waitFor(async () => (await bell.getAttribute('aria-label')) !== bellBefore, 5000), `${bellBefore} → ${await bell.getAttribute('aria-label')}`)

  // Command palette search.
  await demo.page.keyboard.press('Control+k')
  await demo.page.keyboard.type(identifier)
  t.ok('palette: finds the new issue', await waitFor(() => demo.page.getByRole('option', { name: new RegExp(`\\b${identifier}\\b`) }).first().isVisible(), 5000))
  await demo.page.keyboard.press('Escape')

  // Live description editing with presence, both on the issue page.
  await maya.page.goto(`${B}/${WORKSPACE}/issue/${identifier}`)
  const description = (p) => p.getByRole('textbox', { name: 'Description' })
  await description(demo.page).locator('[data-type=mention]').waitFor()
  t.ok('presence: demo sees maya viewing', await waitFor(() => demo.page.getByRole('img', { name: /^Also viewing: Maya/ }).isVisible(), 5000))
  await waitFor(async () => (await description(maya.page).getAttribute('contenteditable')) === 'true', 5000)
  await description(demo.page).click()
  await demo.page.keyboard.press('Control+End')
  await demo.page.keyboard.type(' live')
  t.ok('live editing: maya sees demo typing', await waitFor(async () => (await description(maya.page).innerText()).includes('live'), 5000))
  t.ok('live editing: named caret on maya side', await waitFor(async () => (await maya.page.locator('.collaboration-carets__label').allInnerTexts()).some((s) => s.includes(DEMO.name))))

  // A comment with an attachment, rendered for maya without an editor per comment.
  const chooser = demo.page.waitForEvent('filechooser')
  await demo.page.getByRole('button', { name: 'Attach file' }).last().click()
  await (await chooser).setFiles({ name: 'pink.png', mimeType: 'image/png', buffer: PNG })
  await demo.page.getByRole('textbox', { name: 'Comment', exact: true }).locator('img').waitFor()
  await demo.page.getByRole('textbox', { name: 'Comment', exact: true }).click()
  await demo.page.keyboard.press('Control+End')
  await demo.page.keyboard.type('e2e comment')
  await demo.page.getByRole('button', { name: 'Comment', exact: true }).click()
  const mayaComment = maya.page.getByRole('article', { name: /^Comment by Demo/ }).filter({ hasText: 'e2e comment' })
  t.ok('comments: maya sees the comment and its image', await waitFor(async () => (await mayaComment.locator('img[src^="/api/files/"]').count()) === 1, 5000))
  t.ok('comments: read-only comments mount no editor', (await mayaComment.locator('.ProseMirror').count()) === 0 && (await mayaComment.locator('.rich-text').count()) === 1)

  // Renderer parity: a doc using every node and mark, read-only (JSON renderer) vs in the edit form (Tiptap).
  const upload = await (await demo.req.post(`${API}/files`, multipart({ file: { name: 'pink.png', mimeType: 'image/png', buffer: PNG } }))).json()
  const text = (s, ...marks) => ({ type: 'text', text: s, ...(marks.length ? { marks } : {}) })
  const para = (...content) => ({ type: 'paragraph', content })
  const sample = {
    type: 'doc',
    content: [
      { type: 'heading', attrs: { level: 1 }, content: [text('Heading one')] },
      para(text('plain '), text('bold ', { type: 'bold' }), text('both', { type: 'bold' }, { type: 'italic' }), text(' italic', { type: 'italic' }), text(' '), text('struck', { type: 'strike' }), text(' '), text('code', { type: 'code' }), text(' '), text('under', { type: 'underline' })),
      para(text('see '), text('the site', { type: 'link', attrs: { href: 'https://example.com/a?b=1' } }), text(' and '), text('mail', { type: 'link', attrs: { href: 'mailto:x@example.com' } }, { type: 'bold' }), { type: 'hardBreak' }, text('hi '), { type: 'mention', attrs: { id: issue.assignee.id, label: 'Maya Member' } }, text(' there  two spaces')),
      { type: 'heading', attrs: { level: 3 }, content: [text('Lists')] },
      { type: 'bulletList', content: [{ type: 'listItem', content: [para(text('one'))] }, { type: 'listItem', content: [para(text('two'))] }] },
      { type: 'orderedList', attrs: { start: 3 }, content: [{ type: 'listItem', content: [para(text('three'))] }] },
      { type: 'blockquote', content: [para(text('quoted'))] },
      { type: 'codeBlock', attrs: { language: 'js' }, content: [text('const a  = 1\n  b()')] },
      { type: 'horizontalRule' },
      { type: 'image', attrs: { src: upload.url, alt: 'pink.png' } },
      para(text('end')),
    ],
  }
  const posted = await demo.req.post(`${API}/issues/${identifier}/comments`, multipart({ body: JSON.stringify(sample) }))
  t.ok('renderer: sample doc accepted by the API', posted.ok(), String(posted.status()))
  const card = demo.page.getByRole('article', { name: /^Comment by Demo/ }).filter({ hasText: 'Heading one' })
  await card.locator('.rich-text').waitFor()
  const rendered = await shape(card.locator('.rich-text'))
  await card.getByRole('button', { name: 'Comment actions' }).click()
  await demo.page.locator('.popover').getByRole('button', { name: 'Edit', exact: true }).click()
  const editor = card.getByRole('textbox', { name: 'Edit comment' })
  await editor.locator('h1').waitFor()
  const edited = await shape(editor)
  t.ok('renderer: JSON renderer matches the editor DOM', rendered === edited, rendered === edited ? `${rendered.split('\n').length} nodes` : `\n--- renderer\n${rendered}\n--- editor\n${edited}`)
  await card.getByRole('button', { name: 'Cancel' }).click()

  // Offline: a failed request says so instead of the generic error.
  await demo.ctx.setOffline(true)
  await demo.page.getByRole('textbox', { name: 'Comment', exact: true }).click()
  await demo.page.keyboard.type('offline try')
  await demo.page.getByRole('button', { name: 'Comment', exact: true }).click()
  t.ok('offline: distinct message', await waitFor(() => demo.page.getByText("You're offline. Reconnect and try again.").isVisible()))
  await demo.ctx.setOffline(false)
  await demo.page.getByRole('textbox', { name: 'Comment', exact: true }).click()
  await demo.page.keyboard.press('Control+a')
  await demo.page.keyboard.press('Backspace')

  // Board: pointer drag, then Shift+→ from the keyboard.
  const statuses = await (await demo.req.get(`${API}/statuses`)).json()
  const statusOf = async () => (await (await demo.req.get(`${API}/issues/${identifier}`)).json()).status.id
  const nextStatus = (id, d = 1) => statuses[statuses.findIndex((s) => s.id === id) + d]
  await demo.page.goto(`${B}/${WORKSPACE}/projects/${PROJECT}?view=board`)
  const cardEl = demo.page.locator(`.board-card[data-issue="${issue.id}"]`)
  await cardEl.waitFor()
  const from = await statusOf()
  const target = nextStatus(from)
  const col = demo.page.locator(`[data-column="${target.id}"]`)
  await col.scrollIntoViewIfNeeded()
  await cardEl.scrollIntoViewIfNeeded()
  const a = await cardEl.boundingBox()
  const b = await col.boundingBox()
  await demo.page.mouse.move(a.x + a.width / 2, a.y + a.height / 2)
  await demo.page.mouse.down()
  await demo.page.mouse.move(a.x + a.width / 2 + 20, a.y + a.height / 2, { steps: 4 })
  await demo.page.mouse.move(b.x + b.width / 2, a.y + a.height / 2, { steps: 12 })
  await demo.page.mouse.up()
  t.ok('board: drag moves the card to the next column', await waitFor(async () => (await statusOf()) === target.id), `${statuses.find((s) => s.id === from).name} → ${target.name}`)

  await demo.page.locator(`[data-column="${target.id}"] .board-card[data-issue="${issue.id}"]`).focus()
  await demo.page.keyboard.press('Shift+ArrowRight')
  const after = nextStatus(target.id)
  t.ok('board: Shift+→ moves the focused card', await waitFor(async () => (await statusOf()) === after.id), `→ ${after.name}`)
  t.ok('board: move announced', await waitFor(async () => (await demo.page.locator('.board [role=status]').innerText()) === `Moved ${identifier} to ${after.name}.`))
  t.ok('board: focus follows the card', await waitFor(() => demo.page.evaluate((id) => document.activeElement?.getAttribute('data-issue') === id && !!document.activeElement.closest(`[data-column]`), issue.id)))
  await demo.page.keyboard.press('Shift+ArrowLeft')
  t.ok('board: Shift+← moves it back', await waitFor(async () => (await statusOf()) === target.id))

  // Single-key shortcuts can be turned off in Settings → Profile; Ctrl+K keeps working.
  await demo.page.goto(`${B}/${WORKSPACE}/settings/profile`)
  const toggle = demo.page.getByRole('checkbox', { name: 'Single-key shortcuts' })
  await toggle.uncheck()
  await demo.page.goto(`${B}/${WORKSPACE}/projects/${PROJECT}`)
  await demo.page.locator('h1').waitFor()
  await demo.page.keyboard.press('c')
  await sleep(400)
  const createOpen = () => demo.page.getByRole('dialog', { name: 'New issue' }).isVisible()
  t.ok('shortcuts: "c" does nothing when turned off', !(await createOpen()))
  await demo.page.keyboard.press('Control+k')
  t.ok('shortcuts: Ctrl+K still opens the palette', await waitFor(() => demo.page.locator('dialog.palette[open]').isVisible()))
  await demo.page.keyboard.press('Escape')
  await demo.page.goto(`${B}/${WORKSPACE}/settings/profile`)
  await toggle.check()
  await demo.page.goto(`${B}/${WORKSPACE}/projects/${PROJECT}`)
  await demo.page.locator('h1').waitFor()
  await demo.page.keyboard.press('c')
  t.ok('shortcuts: "c" works again when turned on', await waitFor(createOpen))
  await demo.page.keyboard.press('Escape')

  // Timeline pages: over 100 items shows "Show older activity", which prepends the rest.
  const first = await (await demo.req.get(`${API}/issues/${identifier}/timeline`)).json()
  if (!('has_more' in first)) {
    t.skip('timeline: older pages', 'API has no has_more yet')
  } else {
    for (let i = 0; i < 110; i++) await demo.req.post(`${API}/issues/${identifier}/comments`, multipart({ body: JSON.stringify({ type: 'doc', content: [para(text(`bulk ${i}`))] }) }))
    await demo.page.goto(`${B}/${WORKSPACE}/issue/${identifier}`)
    const older = demo.page.getByRole('button', { name: 'Show older activity' })
    t.ok('timeline: "Show older activity" when there is more', await waitFor(() => older.isVisible(), 5000))
    const count = () => demo.page.getByRole('article', { name: /^Comment by/ }).count()
    const before = await count()
    await older.click()
    t.ok('timeline: older items prepended', await waitFor(async () => (await count()) > before && !(await older.count()), 5000), `${before} → ${await count()} comments`)
    t.ok('timeline: oldest item first', (await demo.page.locator('ol > li').first().innerText()).includes('created the issue'))
  }
} catch (e) {
  t.ok('run', false, e.message.split('\n')[0])
  await demo.page.screenshot({ path: shot('e2e-fail-demo.png') }).catch(() => {})
  await maya.page.screenshot({ path: shot('e2e-fail-maya.png') }).catch(() => {})
} finally {
  if (issue) await demo.req.delete(`${API}/issues/${issue.identifier}`)
  await browser.close()
  t.ok('no page errors', errors.length === 0, errors.join('; '))
  t.done()
}
