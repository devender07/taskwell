#!/usr/bin/env node
// taskwell-tester: a stdio MCP server. The user's coding agent decides each action; this drives Chrome on their
// machine and sends every step, screenshot and a video of each case to Taskwell (docs/local-test-runner-plan.md).
import { mkdtemp, readFile, rm } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { Server } from '@modelcontextprotocol/sdk/server/index.js'
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js'
import { CallToolRequestSchema, ListToolsRequestSchema } from '@modelcontextprotocol/sdk/types.js'
import { chromium } from 'playwright-core'

const BASE = (process.env.TASKWELL_URL || 'https://api.taskwell.dev').replace(/\/+$/, '')
const TOKEN = process.env.TASKWELL_TOKEN || ''
const HEADLESS = process.env.TASKWELL_HEADLESS === '1'
const SIZE = { width: 1280, height: 720 }
const MAX_VIDEO = 10 << 20
const MAX_STEPS = 60 // Taskwell's cap per case; checked here so the action isn't done and then refused
const MAX_TEXT = 4000
const KEYS = ['Enter', 'Tab', 'Escape', 'Backspace', 'Delete', 'Space', 'ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight',
  'PageUp', 'PageDown', 'Home', 'End']
// The install commands name this playwright-core's version: each version wants its own browser and ffmpeg builds.
const PW = 'npx -y playwright-core@' + createRequire(import.meta.url)('playwright-core/package.json').version

let run = null // { id, slug, path, ids }: one run at a time
let browser = null
let context = null // the open case's browser context
let page = null
let video = null // the case's first tab's
let dir = null // where the case's video is written
let current = null // the open case's result_id
let steps = 0 // posted for the open case
let elements = [] // the latest page's, by ref - 1

const oneLine = (e) => String(e?.message ?? e).split('\n')[0]
const enc = encodeURIComponent

function fields(o) {
  const f = new FormData()
  for (const [k, v] of Object.entries(o)) f.set(k, String(v))
  return f
}

// taskwell posts a multipart form to the API and returns its JSON. A 409 during a run means Taskwell ended it
// (cancelled, finished or stale): the run and the browser are closed here.
async function taskwell(path, form) {
  const res = await fetch(BASE + '/api' + path, { method: 'POST', headers: { Authorization: 'Bearer ' + TOKEN }, body: form })
  const data = await res.json().catch(() => ({}))
  if (res.ok) return data
  const msg = data.error || Object.values(data.errors ?? {}).join(' ') || `Taskwell answered ${res.status}.`
  if (res.status === 409 && run) {
    await stop()
    throw new Error(`Taskwell ended this run: ${msg} The browser is closed. Tell the user; start_test_run starts a new run.`)
  }
  throw new Error(msg)
}

async function closeCase() {
  const c = context
  context = page = current = null
  elements = []
  await c?.close().catch(() => {}) // finalises the video
}

async function dropDir() {
  if (dir) await rm(dir, { recursive: true, force: true }).catch(() => {})
  dir = video = null
}

async function stop() {
  run = null
  await closeCase()
  await dropDir()
  await browser?.close().catch(() => {})
  browser = null
}

// openCase opens a fresh context that records video, launching the browser when it isn't up: the user's Chrome,
// else Playwright's Chromium.
async function openCase() {
  if (!browser?.isConnected()) {
    browser = await chromium.launch({ channel: 'chrome', headless: HEADLESS }).catch((chrome) =>
      chromium.launch({ headless: HEADLESS }).catch(() => {
        throw new Error(`Couldn't open Chrome (${oneLine(chrome)}). Install Google Chrome, or run: ${PW} install chromium`)
      }))
  }
  dir = await mkdtemp(join(tmpdir(), 'taskwell-tester-'))
  try {
    context = await browser.newContext({ viewport: SIZE, recordVideo: { dir, size: SIZE } })
    // ponytail: a tab the page opens is followed, but only the first tab's video is uploaded; stitch or upload each
    // if cases that end in a second tab turn out common.
    const watch = (p) => {
      page = p
      p.on('dialog', (d) => d.accept().catch(() => {}))
      p.on('close', () => { page = context?.pages().at(-1) ?? page })
    }
    context.on('page', watch)
    page = await context.newPage()
    video = page.video()
  } catch (e) {
    await closeCase()
    await dropDir()
    // Playwright records with its own ffmpeg build, which the system Chrome doesn't bring.
    if (/ffmpeg/i.test(e.message)) throw new Error(`Video recording needs Playwright's ffmpeg. Run once: ${PW} install ffmpeg`)
    throw e
  }
}

// settle waits for the page to finish loading, then a moment for scripts to render.
async function settle() {
  await page.waitForLoadState('load', { timeout: 10_000 }).catch(() => {})
  await page.waitForTimeout(400).catch(() => {})
}

// readPage runs in the page: it tags the visible interactive elements with data-taskwell-ref and lists them, never
// a password's value. The same shape as the hosted browser tools (backend/user/internal/aitest/cdp.go stateJS).
function readPage() {
  const sel = 'a[href],button,input,select,textarea,summary,[role=button],[role=link],[role=checkbox],[role=radio],[role=tab],[role=menuitem],[role=option],[role=switch],[contenteditable]:not([contenteditable=false]),[tabindex]:not([tabindex="-1"]),[onclick]'
  const clean = (s) => String(s || '').trim().replace(/\s+/g, ' ').slice(0, 80)
  const visible = (el) => {
    const r = el.getBoundingClientRect(), st = getComputedStyle(el)
    return r.width > 0 && r.height > 0 && st.visibility !== 'hidden' && st.display !== 'none' && el.type !== 'hidden'
  }
  document.querySelectorAll('[data-taskwell-ref]').forEach((e) => e.removeAttribute('data-taskwell-ref'))
  const out = []
  for (const el of document.querySelectorAll(sel)) {
    if (out.length >= 200) break
    if (!visible(el)) continue
    const n = out.length + 1
    el.setAttribute('data-taskwell-ref', String(n))
    const tag = el.tagName.toLowerCase(), type = (el.getAttribute('type') || 'text').toLowerCase()
    const kind = el.getAttribute('role') || (tag === 'input' ? 'input type=' + type : tag)
    const name = clean(el.getAttribute('aria-label') || (el.labels && el.labels[0] && el.labels[0].innerText) || el.placeholder ||
      el.innerText || (tag === 'input' && (type === 'submit' || type === 'button') ? el.value : '') || el.title || el.name)
    const label = kind + ' "' + name + '"'
    let line = '[' + n + '] ' + label
    if (tag === 'textarea' || (tag === 'input' && !['password', 'checkbox', 'radio', 'submit', 'button', 'file'].includes(type))) line += ' value="' + clean(el.value) + '"'
    if (type === 'checkbox' || type === 'radio') line += el.checked ? ' checked' : ' unchecked'
    if (tag === 'select') line += ' selected="' + clean(el.selectedOptions[0] && el.selectedOptions[0].text) + '" options=' + JSON.stringify([...el.options].slice(0, 30).map((o) => clean(o.text)))
    if (el.disabled) line += ' disabled'
    if (tag === 'a') line += ' href="' + String(el.getAttribute('href')).slice(0, 120) + '"'
    out.push({ line, label, password: tag === 'input' && type === 'password', select: tag === 'select' })
  }
  return { url: location.href, title: document.title, text: document.body ? document.body.innerText.slice(0, 6000) : '', elements: out }
}

// describePage is the page for the agent, after note: URL, title, numbered interactive elements and visible text.
async function describePage(note) {
  const s = await page.evaluate(readPage).catch(async () => { // the page navigated under the script
    await settle()
    return page.evaluate(readPage)
  })
  elements = s.elements
  let text = `${note}\n\nURL: ${s.url}\nTitle: ${s.title}\n\nInteractive elements:\n` +
    `${s.elements.map((e) => e.line).join('\n') || '(none)'}\n\nVisible text:\n${s.text.slice(0, MAX_TEXT)}`
  // A page that shows a typed password (a "show password" toggle) must not hand it to the agent.
  for (const [k, v] of Object.entries(process.env)) if (k.startsWith('TASKWELL_PASSWORD_') && v) text = text.replaceAll(v, '[password]')
  return text
}

function element(ref) {
  const el = elements[ref - 1]
  if (!el) throw new Error(`No element [${ref}] on the page. Use a ref from the latest page.`)
  return { ...el, loc: page.locator(`[data-taskwell-ref="${ref}"]`) }
}

async function assertText(text, want) {
  text = String(text ?? '').trim()
  if (!text || text.length > 200) throw new Error('Give the text to look for, up to 200 characters.')
  await page.waitForFunction(([t, w]) => (!!document.body && document.body.innerText.includes(t)) === w, [text, want], { timeout: 5000 })
    .catch(() => { throw new Error(`Expected "${text}" ${want ? '' : 'not to be '}on the page.`) })
  return `Checked "${text}" is ${want ? '' : 'not '}on the page`
}

// Each action does its work in the browser and returns the step's description for Taskwell, or throws why it failed.
const actions = {
  async browser_navigate({ url }) {
    let u
    try { u = new URL(url) } catch {}
    if (u?.protocol !== 'http:' && u?.protocol !== 'https:') throw new Error('Give an http or https URL.')
    await page.goto(u.href, { timeout: 30_000 }).catch((e) => { throw new Error("Couldn't load the page: " + oneLine(e)) })
    return 'Went to ' + u.href
  },
  async browser_click({ ref }) {
    const el = element(ref)
    await el.loc.click({ timeout: 5000 })
    return 'Clicked ' + el.label
  },
  async browser_type({ ref, text }) {
    const el = element(ref)
    text = String(text ?? '')
    await el.loc.fill(text, { timeout: 5000 })
    return 'Typed into ' + el.label // never the text: it may be a secret, and the hosted log leaves it out too
  },
  async browser_select({ ref, value }) {
    const el = element(ref)
    if (!el.select) throw new Error(`[${ref}] isn't a select.`)
    await el.loc.selectOption(String(value), { timeout: 2000 }).catch(() => { throw new Error(`[${ref}] has no option "${value}".`) })
    return `Selected "${value}" in ${el.label}`
  },
  async browser_press({ key }) {
    if (!KEYS.includes(key)) throw new Error('Unknown key. Use one of ' + KEYS.join(', ') + '.')
    await page.keyboard.press(key)
    return 'Pressed ' + key
  },
  async browser_wait({ seconds }) {
    const s = Math.min(Math.max(Math.round(Number(seconds)) || 1, 1), 5)
    await page.waitForTimeout(s * 1000)
    return `Waited ${s}s`
  },
  async browser_fill_password({ ref, account }) {
    const el = element(ref)
    const name = 'TASKWELL_PASSWORD_' + String(account ?? '').toUpperCase().replace(/[^A-Z0-9]+/g, '_').replace(/^_+|_+$/g, '')
    const pw = process.env[name]
    if (!pw) throw new Error(`No password for "${account}". Ask the user to set ${name} in taskwell-tester's MCP config.`)
    if (!el.password) throw new Error('Passwords go only into password fields.')
    await el.loc.fill(pw, { timeout: 5000 })
    return 'Typed the password for ' + account
  },
  browser_assert_text: ({ text }) => assertText(text, true),
  browser_assert_no_text: ({ text }) => assertText(text, false),
}

function openPage() {
  if (!run) throw new Error('No test run. Call start_test_run first.')
  if (!current) throw new Error('No case is open. Call begin_case with a result_id.')
}

const failed = (text) => ({ content: [{ type: 'text', text }], isError: true })

// act does one action; when it works, posts it to Taskwell as a step with a screenshot. Answers with the page.
async function act(name, args) {
  openPage()
  if (steps >= MAX_STEPS) throw new Error(`A test case can have ${MAX_STEPS} steps. Call finish_case.`)
  let action
  try {
    action = await actions[name](args)
    await settle()
  } catch (e) {
    const now = await describePage('').catch(() => '\n\nThe browser page is gone. Call begin_case again to restart this case.')
    return failed('Failed, nothing recorded: ' + oneLine(e) + now)
  }
  const form = fields({ action: action.slice(0, 300), url: page.url().slice(0, 2000) })
  form.set('screenshot', new Blob([await page.screenshot({ type: 'jpeg', quality: 60 })], { type: 'image/jpeg' }), 'step.jpg')
  try {
    await taskwell(`${run.path}/results/${current}/steps`, form)
  } catch (e) {
    if (!run) throw e
    throw new Error('Done in the browser, but Taskwell did not record the step: ' + oneLine(e))
  }
  return describePage(`Recorded steps: ${++steps}`)
}

// sendVideo uploads the case's video the way the app uploads a file (start, PUT to the bucket, then hand the upload's
// id to the endpoint that keeps it) and answers what to add to the result's note: nothing when it worked.
async function sendVideo(resultID) {
  const bytes = await readFile(await video.path())
  // ponytail: a video over 10 MB is dropped; lower the frame size or split by step if that turns out common.
  if (bytes.length > MAX_VIDEO) return ' (The video was over 10 MB and was not uploaded.)'
  const up = await taskwell(`/workspaces/${enc(run.slug)}/uploads`, fields({ name: 'test-case.webm', size: bytes.length, type: 'video/webm' }))
  const put = await fetch(up.url, { method: 'PUT', headers: up.headers, body: bytes })
  if (!put.ok) throw new Error(`the file store answered ${put.status}`)
  await taskwell(`${run.path}/results/${resultID}/video`, fields({ upload_id: up.id }))
  return ''
}

const ref = { type: 'integer', description: "the element's number in the latest page" }
const obj = (properties, required = Object.keys(properties)) => ({ type: 'object', properties, required })
const str = (description) => ({ type: 'string', description })
const pageAnswer = 'Answers with the page after it: URL, title, numbered interactive elements and the visible text.'
const step = (name, description, properties) => ({ name, description: description + ' ' + pageAnswer, inputSchema: obj(properties), run: (a) => act(name, a) })

const tools = [
  {
    name: 'start_test_run',
    description: "Start testing an issue's test cases in Chrome on this machine. You decide every action; this tool drives the " +
      'browser and sends each step, a screenshot of it and a video of each case to Taskwell, where the team sees the run live on ' +
      'the issue. It answers with the run and, for each case, its result_id, steps and expected result. Then, for each case in ' +
      'order: (1) begin_case with its result_id and the URL to start at (localhost works: the browser is on this machine); ' +
      '(2) follow the case\'s steps with browser_click, browser_type, browser_select, browser_press, browser_wait and ' +
      'browser_navigate. Every answer is the page: URL, title, numbered interactive elements (the number is the ref those tools ' +
      'take, valid for the latest page only) and the visible text; (3) check the expected result with browser_assert_text and ' +
      'browser_assert_no_text; (4) finish_case with passed, failed or skipped. A case fails when its expected result does not ' +
      'hold or a step cannot be done: finish it as failed with what happened and reproduction steps a person can follow. Do not ' +
      'retry until it passes and do not fix the app mid-run. Skip a case only when it cannot be tested here, and say why. ' +
      'Passwords: never ask the user for one and never type one with browser_type. browser_fill_password types a test ' +
      "account's password from this machine's TASKWELL_PASSWORD_<LABEL> setting; you never see it. An action that fails " +
      'records nothing and answers why. The run finishes with its last case; cancel_test_run stops it early. One run at a time.',
    inputSchema: obj({
      workspace: str('workspace slug'), identifier: str('issue identifier, such as ENG-142'),
      case_id: str('only this test case; every case on the issue when left out'),
    }, ['workspace', 'identifier']),
    async run({ workspace, identifier, case_id }) {
      if (run) throw new Error(`Run ${run.id} is still open. Finish its cases or call cancel_test_run.`)
      if (!TOKEN) throw new Error("TASKWELL_TOKEN isn't set. Ask the user to add their Taskwell API token to taskwell-tester's MCP config.")
      await openCase() // before the run exists: a browser or video problem shouldn't leave a run behind
      await closeCase()
      await dropDir()
      const form = fields({ kind: 'local', client: (server.getClientVersion()?.name || 'MCP client').slice(0, 40) })
      if (case_id) form.set('case_id', case_id)
      const r = await taskwell(`/workspaces/${enc(workspace)}/issues/${enc(identifier)}/test-runs`, form)
      run = { id: r.id, slug: workspace, path: `/workspaces/${enc(workspace)}/test-runs/${r.id}`, ids: r.results.map((x) => x.id) }
      return `Run ${r.id} started on ${r.issue?.identifier ?? identifier}${r.issue?.title ? ` "${r.issue.title}"` : ''}: ` +
        `${r.results.length} case(s). For each, in order: begin_case, the browser tools, finish_case.\n\n` +
        r.results.map((x, i) => `Case ${i + 1}: ${x.title}\nresult_id: ${x.id}\nSteps:\n` +
          `${(x.steps ?? []).map((s, n) => `${n + 1}. ${s}`).join('\n') || '(none written: test the expected result)'}\nExpected: ${x.expected}`).join('\n\n')
    },
  },
  {
    name: 'begin_case',
    description: 'Open a fresh browser (no cookies or storage from the last case), recording video, for one case and go to the ' +
      'URL. ' + pageAnswer + ' Calling it again for the open case starts that case over.',
    inputSchema: obj({ result_id: str("the case's result_id from start_test_run"), url: str('where the case starts, http or https; localhost works') }),
    async run({ result_id, url }) {
      if (!run) throw new Error('No test run. Call start_test_run first.')
      if (!run.ids.includes(result_id)) throw new Error('Unknown result_id. Use one from start_test_run.')
      if (current && current !== result_id) throw new Error(`Case ${current} is still open. Call finish_case for it first.`)
      await closeCase()
      await dropDir()
      await openCase()
      current = result_id
      steps = 0
      const out = await act('browser_navigate', { url })
      if (out.isError) { // nothing to test on: the case stays unopened
        await closeCase()
        await dropDir()
      }
      return out
    },
  },
  step('browser_navigate', 'Go to a URL (http or https).', { url: str('the URL') }),
  step('browser_click', 'Click the element with the ref.', { ref }),
  step('browser_type', "Type text into the element with the ref, replacing what's in it. Never a password: use browser_fill_password.",
    { ref, text: str('the text') }),
  step('browser_select', 'Choose an option, by value or text, in the select with the ref.', { ref, value: str("the option's value or text") }),
  step('browser_press', `Press a key: ${KEYS.join(', ')}.`, { key: str('such as Enter') }),
  step('browser_wait', 'Wait 1-5 seconds for the page to change.', { seconds: { type: 'integer', description: '1-5' } }),
  step('browser_fill_password', "Type a test account's password into the password field with the ref. It comes from " +
    'TASKWELL_PASSWORD_<LABEL> on this machine; you never see it, and Taskwell is told only that it was typed.',
  { ref, account: str("the test account's label, such as admin (TASKWELL_PASSWORD_ADMIN)") }),
  step('browser_assert_text', 'Check the text is on the page (waits up to 5 s). When it is not, nothing is recorded and the ' +
    'answer says so: that is how an expected result fails.', { text: str('up to 200 characters') }),
  step('browser_assert_no_text', 'Check the text is not on the page (waits up to 5 s for it to go). When it is still there, ' +
    'nothing is recorded and the answer says so.', { text: str('up to 200 characters') }),
  {
    name: 'browser_snapshot',
    description: 'Read the page again; records nothing.',
    inputSchema: obj({}),
    annotations: { readOnlyHint: true },
    async run() {
      openPage()
      return describePage(`Recorded steps: ${steps}`)
    },
  },
  {
    name: 'browser_screenshot',
    description: 'See the page as an image; records nothing.',
    inputSchema: obj({}),
    annotations: { readOnlyHint: true },
    async run() {
      openPage()
      const shot = await page.screenshot({ type: 'jpeg', quality: 60 })
      return { content: [{ type: 'image', data: shot.toString('base64'), mimeType: 'image/jpeg' }] }
    },
  },
  {
    name: 'finish_case',
    description: "Record a case's outcome: closes its browser, uploads its video and marks the result in Taskwell. passed: " +
      'every step worked and the expected result holds. failed: it does not; give reproduction, the steps a person follows ' +
      'to see the failure, one per line. skipped: it could not be tested here (no begin_case needed); say why in summary. ' +
      'Answers with the next case to test, or the totals when this was the last one and the run is finished.',
    inputSchema: obj({
      result_id: str("the case's result_id"), status: { type: 'string', enum: ['passed', 'failed', 'skipped'] },
      summary: str('what happened, in a sentence or two; up to 2000 characters'),
      reproduction: { type: 'array', items: { type: 'string' }, description: 'for a failure: the steps to reproduce it, up to 30 lines of 300 characters' },
    }, ['result_id', 'status', 'summary']),
    async run({ result_id, status, summary, reproduction }) {
      if (!run) throw new Error('No test run. Call start_test_run first.')
      if (!run.ids.includes(result_id)) throw new Error('Unknown result_id. Use one from start_test_run.')
      if (current && current !== result_id) throw new Error(`Case ${current} is the open one. Finish it first.`)
      if (!['passed', 'failed', 'skipped'].includes(status)) throw new Error('status is passed, failed or skipped.')
      const lines = (Array.isArray(reproduction) ? reproduction : []).map((l) => String(l).trim().slice(0, 300)).filter(Boolean).slice(0, 30)
      if (status === 'failed' && !lines.length) throw new Error('A failed case needs reproduction: the steps to see the failure.')
      let extra = ''
      if (current) {
        await closeCase()
        extra = await sendVideo(result_id).catch((e) => {
          if (!run) throw e // Taskwell ended the run
          return ` (The video was not uploaded: ${oneLine(e)})`
        })
        await dropDir()
      }
      const form = fields({ status, note: String(summary ?? '').trim().slice(0, 2000 - extra.length) + extra })
      for (const l of lines) form.append('reproduction', l)
      const r = await taskwell(`${run.path}/results/${result_id}`, form)
      const left = r.results.filter((x) => x.status === 'pending' || x.status === 'running')
      if (r.status === 'running' && left.length) {
        return `Marked ${status}.${extra} ${left.length} case(s) left. Next: begin_case with result_id ${left[0].id} (${left[0].title}).`
      }
      await stop()
      const c = r.counts ?? {}
      return `Marked ${status}.${extra} The run is finished: ${c.passed ?? 0} passed, ${c.failed ?? 0} failed, ${c.skipped ?? 0} skipped. ` +
        'Tell the user the outcome and what failed.'
    },
  },
  {
    name: 'cancel_test_run',
    description: 'Cancel the open test run and close the browser. Cases not finished stay unmarked.',
    inputSchema: obj({}),
    annotations: { destructiveHint: true },
    async run() {
      if (!run) throw new Error('No test run is open.')
      const { id } = run
      await taskwell(run.path + '/cancel')
      await stop()
      return `Run ${id} cancelled.`
    },
  },
]

const server = new Server({ name: 'taskwell-tester', version: '0.1.0' }, { capabilities: { tools: {} } })

server.setRequestHandler(ListToolsRequestSchema, async () => ({
  tools: tools.map(({ name, description, inputSchema, annotations }) => ({ name, description, inputSchema, annotations })),
}))

let queue = Promise.resolve() // one tool call at a time: they share the browser
server.setRequestHandler(CallToolRequestSchema, ({ params }) => {
  const call = async () => {
    const tool = tools.find((t) => t.name === params.name)
    if (!tool) return failed('Unknown tool.')
    try {
      const out = await tool.run(params.arguments ?? {})
      return typeof out === 'string' ? { content: [{ type: 'text', text: out }] } : out
    } catch (e) {
      return failed(oneLine(e))
    }
  }
  return (queue = queue.then(call, call))
})

// The agent went away: close the browser. The run is left for Taskwell to end as stale.
const quit = () => stop().finally(() => process.exit(0))
server.onclose = quit
process.stdin.on('end', quit)
await server.connect(new StdioServerTransport())
