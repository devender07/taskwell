// The landing page's playable board (README.md): drag a card to Done, move one with the arrow keys, create one with C,
// the nav highlighting the section in view, and replaying the AI test run. Signed out, no data written.
import { USER_URL as B, launch, reporter, shot, sleep, waitFor } from './lib.mjs'

const t = reporter()
const errors = []
const browser = await launch()
const page = await browser.newPage({ viewport: { width: 1280, height: 900 } })
page.on('pageerror', (e) => errors.push(e.message))
const count = (col) => page.locator('.board-col').nth(col).locator('.count').textContent()

try {
  await page.goto(B, { waitUntil: 'networkidle' })
  await waitFor(() => page.locator('.app-frame.is-visible').count())
  await sleep(700) // the reveal transition moves the board while it plays
  await page.addStyleTag({ content: 'html { scroll-behavior: auto !important; }' }) // smooth scrolling shifts drag targets

  await page.locator('.board').evaluate((b) => b.scrollIntoView({ block: 'center' }))
  const todoCard = page.locator('.board-col').nth(0).locator('.card-mock').first()
  await todoCard.dragTo(page.locator('.board-col').nth(2))
  t.ok('board: drag moves a card to Done', (await count(2)) === '3' && (await count(0)) === '2')
  t.ok('board: dropped card is marked done', await page.locator('.board-col').nth(2).locator('.card-mock.is-done', { hasText: 'Invite teammates' }).isVisible())

  const progressCard = page.locator('.board-col').nth(1).locator('.card-mock').first()
  await progressCard.focus()
  await page.keyboard.press('ArrowLeft')
  t.ok('board: arrow key moves a card left, focus kept', (await count(0)) === '3' && (await page.evaluate(() => document.activeElement?.closest('.board-col') === document.querySelectorAll('.board-col')[0])))
  t.ok('board: move announced', /moved to Todo/.test(await page.locator('#board-status').textContent()))

  await page.locator('body').click({ position: { x: 5, y: 5 } })
  await page.keyboard.press('c')
  const input = page.getByLabel('Issue title')
  t.ok('quick create: C opens the form, nothing typed', (await input.isVisible()) && (await input.inputValue()) === '')
  await input.fill('Try Taskwell')
  await page.keyboard.press('Enter')
  t.ok('quick create: card added to Todo', (await page.locator('.board-col').nth(0).locator('.card-mock', { hasText: 'Try Taskwell' }).isVisible()) && (await count(0)) === '4')
  await page.keyboard.press('c')
  await page.keyboard.press('Escape')
  t.ok('quick create: Esc cancels', (await input.count()) === 0)

  await page.locator('#ai').scrollIntoViewIfNeeded()
  t.ok('nav: section in view is current', await waitFor(() => page.locator('.site-nav a[href="#ai"][aria-current]').count().then((n) => n === 1)))
  const replay = page.getByRole('button', { name: 'Run again' })
  t.ok('ai run: replay offered when finished', await waitFor(() => replay.isVisible(), 6000))
  await replay.click()
  t.ok('ai run: replay restarts the steps', (await replay.isHidden()) && (await waitFor(() => replay.isVisible(), 6000)))
} catch (e) {
  t.ok('landing', false, e.message)
  await page.screenshot({ path: shot('e2e-fail-landing.png') }).catch(() => {})
} finally {
  await browser.close()
  t.ok('no page errors', errors.length === 0, errors.join('; '))
  t.done()
}
