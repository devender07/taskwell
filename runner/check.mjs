// node check.mjs: drives the runner's real tools (headless) against a small local page and a fake Taskwell API, and
// asserts what the API received: the run, steps with screenshots, no password, the video upload sequence, the marks.
import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { fileURLToPath } from 'node:url'
import { Client } from '@modelcontextprotocol/sdk/client/index.js'
import { StdioClientTransport } from '@modelcontextprotocol/sdk/client/stdio.js'

const PASSWORD = 's3cret-pw-check'
const TOKEN = 'tth_check'

const listen = (handler) => new Promise((resolve) => {
  const s = createServer(handler).listen(0, '127.0.0.1', () => resolve({ s, url: 'http://127.0.0.1:' + s.address().port }))
})

// --- the page under test: a sign-in form that greets whoever signs in ---
const site = await listen((_, res) => res.setHeader('Content-Type', 'text/html').end(`<!doctype html><title>Check app</title>
  <h1>Sign in</h1>
  <label>Email <input id="email" type="email"></label>
  <label>Password <input id="pw" type="password"></label>
  <select id="plan" aria-label="Plan"><option value="free">Free</option><option value="pro">Pro</option></select>
  <button onclick="document.getElementById('out').textContent = 'Welcome, ' + document.getElementById('email').value + ' on ' + document.getElementById('plan').value + ' (' + document.getElementById('pw').value.length + ')'">Sign in</button>
  <p id="out"></p>`))

// --- the fake Taskwell API: records every request, answers as the plan's contract says ---
const log = [] // { method, path, auth, headers, form, raw }
let runs = 0
let run = null
let ended = false // true: steps answer 409, as after a cancel
const runJSON = () => ({
  ...run,
  status: run.results.some((r) => r.status === 'pending') ? 'running' : 'finished',
  counts: Object.fromEntries(['passed', 'failed', 'skipped'].map((s) => [s, run.results.filter((r) => r.status === s).length])),
})

const api = await listen(async (req, res) => {
  const chunks = []
  for await (const c of req) chunks.push(c)
  const raw = Buffer.concat(chunks)
  const path = req.url
  const entry = { method: req.method, path, auth: req.headers.authorization, headers: req.headers, raw, form: null }
  log.push(entry)
  if ((req.headers['content-type'] ?? '').startsWith('multipart/form-data')) {
    entry.form = await new Request('http://x', { method: 'POST', headers: { 'content-type': req.headers['content-type'] }, body: raw }).formData()
  }
  const send = (status, body) => res.writeHead(status, { 'Content-Type': 'application/json' }).end(JSON.stringify(body))
  if (req.method === 'PUT' && path.startsWith('/bucket/')) return send(200, {})
  if (req.method !== 'POST' || entry.auth !== 'Bearer ' + TOKEN) return send(401, { error: 'Sign in.' })
  let m
  if (path === '/api/workspaces/acme/issues/ENG-1/test-runs') {
    const n = ++runs
    run = {
      id: 'run-' + n, kind: 'local', client: entry.form.get('client'), issue: { identifier: 'ENG-1', title: 'Sign in works' },
      results: [
        { id: `r${n}-pass`, title: 'Sign in greets the user', steps: ['Open the app', 'Sign in as admin'], expected: 'A welcome line shows', status: 'pending' },
        { id: `r${n}-fail`, title: 'Sign in shows the dashboard', steps: [], expected: 'The dashboard shows', status: 'pending' },
      ],
    }
    return send(201, runJSON())
  }
  if ((m = path.match(/^\/api\/workspaces\/acme\/test-runs\/(run-\d+)\/results\/([\w-]+)(\/steps|\/video|)$/)) && m[1] === run.id) {
    const result = run.results.find((r) => r.id === m[2])
    if (!result) return send(404, { error: 'Result not found.' })
    if (ended) return send(409, { error: 'This run is finished.' })
    if (m[3] === '/steps') return send(201, { position: log.filter((e) => e.path === path).length })
    if (m[3] === '') result.status = entry.form.get('status')
    return send(200, runJSON())
  }
  if (path === '/api/workspaces/acme/uploads') {
    const id = 'up-' + log.length
    return send(201, { id, url: `${api.url}/bucket/${id}`, headers: { 'Content-Type': entry.form.get('type'), 'Content-Disposition': 'inline', 'Cache-Control': 'private' } })
  }
  if (path === `/api/workspaces/acme/test-runs/${run?.id}/cancel`) return send(200, runJSON())
  send(404, { error: 'Not found.' })
})

// --- the runner, as an agent would start it ---
const client = new Client({ name: 'check-agent', version: '1' })
await client.connect(new StdioClientTransport({
  command: process.execPath, args: [fileURLToPath(new URL('index.mjs', import.meta.url))], stderr: 'inherit',
  env: { ...process.env, TASKWELL_TOKEN: TOKEN, TASKWELL_URL: api.url, TASKWELL_HEADLESS: '1', TASKWELL_PASSWORD_QA_ADMIN: PASSWORD },
}))

const call = async (name, args = {}) => {
  const r = await client.callTool({ name, arguments: args }, undefined, { timeout: 120_000 })
  return { ...r, text: r.content.find((c) => c.type === 'text')?.text ?? '' }
}
const ok = async (name, args) => {
  const r = await call(name, args)
  assert.ok(!r.isError, `${name} failed: ${r.text}`)
  return r
}
const refOf = (text, name) => Number(text.match(new RegExp(`\\[(\\d+)\\] [^\\n]*"${name}"`))?.[1] ?? assert.fail(`no element "${name}" in:\n${text}`))
const steps = () => log.filter((e) => e.path.endsWith('/steps'))
const actionsOf = (resultID) => steps().filter((e) => e.path.includes(resultID)).map((e) => e.form.get('action'))

try {
  const { tools } = await client.listTools()
  assert.deepEqual(tools.map((t) => t.name), ['start_test_run', 'begin_case', 'browser_navigate', 'browser_click', 'browser_type',
    'browser_select', 'browser_press', 'browser_wait', 'browser_fill_password', 'browser_assert_text', 'browser_assert_no_text',
    'browser_snapshot', 'browser_screenshot', 'finish_case', 'cancel_test_run'])

  assert.match((await call('begin_case', { result_id: 'x', url: site.url })).text, /start_test_run first/)

  // --- a passing case ---
  let r = await ok('start_test_run', { workspace: 'acme', identifier: 'ENG-1' })
  assert.match(r.text, /result_id: r1-pass[\s\S]*1\. Open the app[\s\S]*Expected: A welcome line shows[\s\S]*result_id: r1-fail/)
  assert.match((await call('start_test_run', { workspace: 'acme', identifier: 'ENG-1' })).text, /still open/) // one run at a time

  r = await ok('begin_case', { result_id: 'r1-pass', url: site.url })
  assert.match(r.text, /Recorded steps: 1\n\nURL: http:\/\/127\.0\.0\.1:\d+\/\nTitle: Check app\n\nInteractive elements:\n\[1\] input type=email "Email" value=""/)
  assert.match(r.text, /Visible text:\nSign in/)

  r = await ok('browser_type', { ref: refOf(r.text, 'Email'), text: 'dev@example.com' })
  assert.match(r.text, /"Email" value="dev@example\.com"/)

  const before = steps().length
  r = await call('browser_click', { ref: 99 }) // a failed action: its error, the page, and no step
  assert.ok(r.isError)
  assert.match(r.text, /Failed, nothing recorded: No element \[99\][\s\S]*Interactive elements:/)
  r = await call('browser_fill_password', { ref: refOf(r.text, 'Email'), account: 'QA admin' })
  assert.match(r.text, /Passwords go only into password fields/)
  r = await call('browser_fill_password', { ref: refOf(r.text, 'Password'), account: 'nobody' })
  assert.match(r.text, /TASKWELL_PASSWORD_NOBODY/)
  assert.equal(steps().length, before)

  r = await ok('browser_fill_password', { ref: refOf(r.text, 'Password'), account: 'QA admin' })
  assert.ok(!r.text.includes(PASSWORD))
  r = await ok('browser_select', { ref: refOf(r.text, 'Plan'), value: 'Pro' })
  assert.match(r.text, /selected="Pro"/)
  r = await ok('browser_click', { ref: refOf(r.text, 'Sign in') })
  r = await ok('browser_assert_text', { text: `Welcome, dev@example.com on pro (${PASSWORD.length})` }) // the password was typed
  await ok('browser_assert_no_text', { text: 'Dashboard' })
  await ok('browser_press', { key: 'Tab' })
  await ok('browser_wait', { seconds: 1 })

  const recorded = steps().length
  assert.match((await ok('browser_snapshot')).text, /Recorded steps: 9\n/)
  const shot = (await ok('browser_screenshot')).content[0]
  assert.equal(shot.mimeType, 'image/jpeg')
  assert.ok(Buffer.from(shot.data, 'base64').length > 1000)
  assert.equal(steps().length, recorded) // reading posts nothing

  r = await ok('finish_case', { result_id: 'r1-pass', status: 'passed', summary: 'Signed in and saw the welcome line.' })
  assert.match(r.text, /1 case\(s\) left. Next: begin_case with result_id r1-fail/)

  // --- a failing case ---
  r = await ok('begin_case', { result_id: 'r1-fail', url: site.url })
  r = await call('browser_assert_text', { text: 'Dashboard' })
  assert.ok(r.isError)
  assert.match(r.text, /Expected "Dashboard" on the page/)
  r = await call('finish_case', { result_id: 'r1-fail', status: 'failed', summary: 'No dashboard.' })
  assert.match(r.text, /needs reproduction/)
  r = await ok('finish_case', { result_id: 'r1-fail', status: 'failed', summary: 'No dashboard after signing in.', reproduction: ['Open the app', 'Look for the dashboard'] })
  assert.match(r.text, /The run is finished: 1 passed, 1 failed, 0 skipped/)

  // --- what Taskwell received ---
  const posts = log.filter((e) => e.method === 'POST')
  assert.ok(posts.every((e) => e.auth === 'Bearer ' + TOKEN))
  assert.ok(log.every((e) => !e.raw.includes(PASSWORD)), 'the password left the machine')

  const created = posts[0]
  assert.equal(created.path, '/api/workspaces/acme/issues/ENG-1/test-runs')
  assert.deepEqual([...created.form.entries()], [['kind', 'local'], ['client', 'check-agent']])

  assert.deepEqual(actionsOf('r1-pass'), [
    `Went to ${site.url}/`, 'Typed into input type=email "Email"', 'Typed the password for QA admin',
    'Selected "Pro" in select "Plan"', 'Clicked button "Sign in"',
    `Checked "Welcome, dev@example.com on pro (${PASSWORD.length})" is on the page`, 'Checked "Dashboard" is not on the page',
    'Pressed Tab', 'Waited 1s',
  ])
  assert.deepEqual(actionsOf('r1-fail'), [`Went to ${site.url}/`])
  for (const s of steps()) {
    assert.equal(s.form.get('url'), site.url + '/')
    const jpeg = Buffer.from(await s.form.get('screenshot').arrayBuffer())
    assert.equal(s.form.get('screenshot').type, 'image/jpeg')
    assert.ok(jpeg.length > 1000 && jpeg[0] === 0xff && jpeg[1] === 0xd8, 'a step without a JPEG screenshot')
  }

  // Each case: start the upload, PUT the WebM to the bucket, hand it to the video endpoint, then mark.
  const tail = (id) => log.slice(log.findLastIndex((e) => e.path.endsWith(id + '/steps')) + 1).slice(0, 4)
  for (const [id, status, note, repro] of [
    ['r1-pass', 'passed', 'Signed in and saw the welcome line.', []],
    ['r1-fail', 'failed', 'No dashboard after signing in.', ['Open the app', 'Look for the dashboard']],
  ]) {
    const [start, put, video, mark] = tail(id)
    assert.equal(`${start.method} ${start.path}`, 'POST /api/workspaces/acme/uploads')
    assert.equal(start.form.get('name'), 'test-case.webm')
    assert.equal(start.form.get('type'), 'video/webm')
    assert.equal(`${put.method} ${put.path}`, `PUT /bucket/up-${log.indexOf(start) + 1}`)
    assert.equal(put.auth, undefined) // the token never goes to the bucket
    assert.equal(put.headers['content-type'], 'video/webm')
    assert.equal(put.headers['content-disposition'], 'inline')
    assert.equal(put.raw.length, Number(start.form.get('size')))
    assert.ok(put.raw.length > 1000 && put.raw.subarray(0, 4).equals(Buffer.from([0x1a, 0x45, 0xdf, 0xa3])), 'not a WebM')
    assert.equal(video.path, `/api/workspaces/acme/test-runs/run-1/results/${id}/video`)
    assert.deepEqual([...video.form.entries()], [['upload_id', `up-${log.indexOf(start) + 1}`]])
    assert.equal(mark.path, `/api/workspaces/acme/test-runs/run-1/results/${id}`)
    assert.equal(mark.form.get('status'), status)
    assert.equal(mark.form.get('note'), note)
    assert.deepEqual(mark.form.getAll('reproduction'), repro)
  }

  // --- Taskwell ends a run (a cancel in the app): the next step's 409 stops the runner ---
  await ok('start_test_run', { workspace: 'acme', identifier: 'ENG-1' })
  r = await ok('begin_case', { result_id: 'r2-pass', url: site.url })
  ended = true
  r = await call('browser_press', { key: 'Tab' })
  assert.ok(r.isError)
  assert.match(r.text, /Taskwell ended this run: This run is finished\./)
  assert.match((await call('browser_snapshot')).text, /No test run/)
  ended = false

  // --- cancel_test_run ---
  await ok('start_test_run', { workspace: 'acme', identifier: 'ENG-1' })
  assert.match((await ok('cancel_test_run')).text, /Run run-3 cancelled/)
  assert.equal(log.at(-1).path, '/api/workspaces/acme/test-runs/run-3/cancel')

  console.log(`ok: ${steps().length} step posts, ${log.filter((e) => e.method === 'PUT').length} videos, ${log.length} requests`)
} finally {
  await client.close()
  site.s.close()
  api.s.close()
}
