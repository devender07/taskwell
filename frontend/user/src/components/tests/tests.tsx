import { Link, useNavigate } from '@tanstack/react-router'
import { startTransition, useActionState, useEffect, useOptimistic, useState, type DragEvent, type ReactNode } from 'react'
import { Button } from '@tether/shared/button'
import { Dialog } from '@tether/shared/dialog'
import { IconButton } from '@tether/shared/icon-button'
import { Input } from '@tether/shared/input'
import { Popover } from '@tether/shared/popover'
import { Textarea } from '@tether/shared/textarea'
import { fullTime, relativeTime } from '@tether/shared/time'
import { testCaseExpected, testCaseSteps, testCaseTitle } from '@tether/shared/validate'
import {
  api,
  can,
  deleteTestCase,
  orderTestCases,
  saveTestCase,
  startTestRun,
  testCasesPath,
  testRunsPath,
  testSitesPath,
  toFormErrors,
  type FieldErrors,
  type Issue,
  type RunStatus,
  type TestCase,
  type TestResult,
  type TestRunSummary,
  type TestSite,
  type TestStatus,
  type Workspace,
} from '@/api'
import { FormError } from '@/components/auth/auth-card'
import type { Prefill } from '@/components/issues/issue-form'
import { refetchPaths, subscribePaths } from '@/realtime'
import './tests.css'

// GET `path` and refetch it whenever the server publishes it. `set` takes this tab's own write's response, since
// the tab skips events for its own changes. A null path loads nothing.
export function useLive<T>(path: string | null) {
  const [state, setState] = useState<{ path: string | null; data: T | null; error: string | null }>({ path, data: null, error: null })
  useEffect(() => {
    if (!path) return
    let live = true
    const load = () =>
      api<T>(path).then(
        (data) => live && setState({ path, data, error: null }),
        (e) => live && setState((s) => ({ path, data: s.path === path ? s.data : null, error: toFormErrors(e).error })),
      )
    load()
    const off = subscribePaths([path], load)
    return () => {
      live = false
      off()
    }
  }, [path])
  const mine = state.path === path
  return { data: mine ? state.data : null, error: mine ? state.error : null, set: (data: T) => setState({ path, data, error: null }) }
}

const RESULT_LABELS: Record<TestStatus, string> = {
  pending: 'Not run',
  running: 'Running',
  passed: 'Passed',
  failed: 'Failed',
  skipped: 'Skipped',
  error: 'Error',
}

export function ResultIcon({ status }: { status: TestStatus | null }) {
  const label = status ? RESULT_LABELS[status] : 'Never run'
  const color = status === 'passed' ? 'var(--success)' : status === 'failed' || status === 'error' ? 'var(--accent)' : 'var(--text-2)'
  return (
    <svg role="img" aria-label={label} width={16} height={16} viewBox="0 0 16 16" fill="none" stroke={color} strokeWidth={1.5} strokeLinecap="round" className="flex-noshrink">
      <title>{label}</title>
      <circle cx={8} cy={8} r={7} strokeDasharray={status ? undefined : '2 2'} />
      {status === 'passed' ? <path d="m5 8 2 2 4-4" /> : null}
      {status === 'failed' ? <path d="m5.5 5.5 5 5m0-5-5 5" /> : null}
      {status === 'skipped' ? <path d="M5 8h6" /> : null}
      {status === 'error' ? <path d="M8 4.5v4m0 3h.01" /> : null}
      {status === 'running' ? <path d="M8 1a7 7 0 0 1 0 14z" fill={color} stroke="none" /> : null}
    </svg>
  )
}

const RUN_LABELS: Record<RunStatus, string> = {
  queued: 'Queued',
  generating: 'Writing cases',
  running: 'Running',
  passed: 'Passed',
  failed: 'Failed',
  error: 'Error',
  cancelled: 'Cancelled',
  limit_reached: 'Limit reached',
}

export const runFinished = (s: RunStatus) => !['queued', 'generating', 'running'].includes(s)

export function RunStatusPill({ status }: { status: RunStatus }) {
  const color = status === 'passed' ? 'var(--success)' : ['failed', 'error', 'limit_reached'].includes(status) ? 'var(--accent)' : 'var(--text-2)'
  return (
    <span className="label-chip flex-noshrink">
      <span className="label-dot" style={{ background: color }} aria-hidden />
      {RUN_LABELS[status]}
    </span>
  )
}

export const formatCost = (usd: number) => `$${usd.toFixed(2)}`

// A bug prefilled from a failed result: its summary, the reproduction (or the case's steps), the expected result and
// the last screenshot. The new issue blocks the tested one.
export function bugPrefill(identifier: string, r: TestResult): Prefill {
  const p = (text: string, bold = false) => ({
    type: 'paragraph',
    ...(text ? { content: [{ type: 'text', text, ...(bold ? { marks: [{ type: 'bold' }] } : {}) }] } : {}),
  })
  const steps = r.reproduction?.length ? r.reproduction : r.steps
  const shot = [...(r.log ?? [])].reverse().find((s) => s.screenshot_url)?.screenshot_url
  return {
    title: `Bug: ${r.title}`,
    blocks: identifier,
    description: {
      type: 'doc',
      content: [
        ...(r.summary ? [p(r.summary)] : []),
        p('Steps to reproduce', true),
        { type: 'orderedList', content: steps.map((s) => ({ type: 'listItem', content: [p(s)] })) },
        p('Expected result', true),
        p(r.expected),
        ...(shot ? [{ type: 'image', attrs: { src: shot, alt: 'Screenshot of the failure' } }] : []),
      ],
    },
  }
}

// A ghost button that asks before running `onConfirm`; errors show in the dialog.
export function ConfirmButton(props: { label: string; title: string; description?: ReactNode; confirm: string; pending: string; onConfirm: () => Promise<unknown> }) {
  const [open, setOpen] = useState(false)
  const [error, run, pending] = useActionState(async () => {
    try {
      await props.onConfirm()
    } catch (e) {
      return toFormErrors(e).error
    }
    setOpen(false)
    return null
  }, null)
  return (
    <>
      <Button variant="ghost" onClick={() => setOpen(true)}>
        {props.label}
      </Button>
      <Dialog open={open} onClose={() => setOpen(false)} title={props.title} description={props.description}>
        <form action={run} className="grid gap-4">
          <FormError>{error}</FormError>
          <Button type="submit" variant="danger" className="w-full" disabled={pending}>
            {pending ? props.pending : props.confirm}
          </Button>
        </form>
      </Dialog>
    </>
  )
}

type Start = { kind: 'ai' | 'manual'; regenerate?: boolean; caseId?: string }

export function TestsSection({ workspace, issue }: { workspace: Workspace; issue: Issue }) {
  const navigate = useNavigate()
  const manage = can(workspace, 'tests.manage')
  const runAi = can(workspace, 'ai_tests.run')
  const cases = useLive<TestCase[]>(testCasesPath(workspace, issue.identifier))
  const runs = useLive<TestRunSummary[]>(testRunsPath(workspace, issue.identifier))
  const sites = useLive<TestSite[]>(runAi ? testSitesPath(workspace) : null)
  const [adding, setAdding] = useState(false)
  const [regenerating, setRegenerating] = useState(false)
  const list = cases.data ?? []
  const noSite = sites.data?.length === 0

  const [startError, start, starting] = useActionState(async (_prev: string | null, { kind, regenerate, caseId }: Start) => {
    let run
    try {
      run = await startTestRun(workspace, issue.identifier, kind, regenerate, caseId)
    } catch (e) {
      return toFormErrors(e).error
    }
    setRegenerating(false)
    refetchPaths([testRunsPath(workspace, issue.identifier)])
    await navigate({ to: '/$workspace/issue/$identifier', params: { workspace: workspace.slug, identifier: issue.identifier }, search: { run: run.id } })
    return null
  }, null)
  const run = (s: Start) => startTransition(() => start(s))

  return (
    <section className="grid gap-4" aria-labelledby="tests-heading">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <h2 id="tests-heading" className="text-label">Tests</h2>
        <div className="flex flex-wrap items-center gap-2">
          {runAi ? (
            <span className="flex items-center gap-1" title={noSite ? 'Add a test site in Settings → AI testing first.' : undefined}>
              <Button variant="secondary" disabled={starting || noSite} onClick={() => run({ kind: 'ai' })}>
                Run AI test
              </Button>
              {list.length > 0 && !noSite ? (
                <Popover
                  aria-label="AI test options"
                  placement="bottom-end"
                  trigger={(props) => (
                    <IconButton aria-label="More AI test options" {...props}>
                      <svg width={20} height={20} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.75} strokeLinecap="round" strokeLinejoin="round" aria-hidden>
                        <path d="m6 9 6 6 6-6" />
                      </svg>
                    </IconButton>
                  )}
                >
                  {(close) => (
                    <Button
                      variant="ghost"
                      className="w-full"
                      onClick={() => {
                        close()
                        setRegenerating(true)
                      }}
                    >
                      Regenerate cases and run
                    </Button>
                  )}
                </Popover>
              ) : null}
            </span>
          ) : null}
          {manage ? (
            <Button variant="ghost" disabled={starting || list.length === 0} onClick={() => run({ kind: 'manual' })}>
              Run manually
            </Button>
          ) : null}
        </div>
      </div>
      {runAi && noSite ? <p className="text-caption text-secondary m-0">Add a test site in Settings → AI testing to run AI tests.</p> : null}
      <FormError>{regenerating ? null : startError}</FormError>
      <FormError>{cases.error}</FormError>
      {cases.data === null && !cases.error ? (
        <p className="text-body-sm text-secondary m-0">Loading…</p>
      ) : list.length === 0 && cases.data ? (
        <p className="text-body-sm text-secondary m-0">No test cases yet. Run an AI test to write them, or add one.</p>
      ) : (
        <CaseList workspace={workspace} issue={issue} cases={list} manage={manage} onChange={cases.set} onRun={runAi && !noSite && !starting ? (id) => run({ kind: 'ai', caseId: id }) : undefined} />
      )}
      {manage ? (
        adding ? (
          <div className="p-6 bg-surface border rounded-lg">
            <CaseForm
              workspace={workspace}
              issue={issue}
              onDone={(saved) => {
                if (saved) cases.set([...list, saved])
                setAdding(false)
              }}
            />
          </div>
        ) : (
          <Button variant="ghost" style={{ justifySelf: 'start' }} onClick={() => setAdding(true)}>
            Add test case
          </Button>
        )
      ) : null}
      <History workspace={workspace} issue={issue} runs={runs.data} error={runs.error} />
      {runAi ? (
        <Dialog open={regenerating} onClose={() => setRegenerating(false)} title="Regenerate cases and run?" description="This replaces the current test cases.">
          <form action={() => start({ kind: 'ai', regenerate: true })} className="grid gap-4">
            <FormError>{startError}</FormError>
            <Button type="submit" variant="danger" className="w-full" disabled={starting}>
              {starting ? 'Starting…' : 'Regenerate and run'}
            </Button>
          </form>
        </Dialog>
      ) : null}
    </section>
  )
}

type ListProps = { workspace: Workspace; issue: Issue; cases: TestCase[]; manage: boolean; onChange: (cases: TestCase[]) => void; onRun?: (caseId: string) => void }

function CaseList({ workspace, issue, cases, manage, onChange, onRun }: ListProps) {
  const [order, setOrder] = useOptimistic(cases)
  const [drag, setDrag] = useState<{ id: string; over: string | null } | null>(null)

  const [moveError, reorder] = useActionState(async (_prev: string | null, next: TestCase[]) => {
    setOrder(next)
    try {
      onChange(await orderTestCases(workspace, issue.identifier, next.map((c) => c.id)))
    } catch (e) {
      return toFormErrors(e).error
    }
    return null
  }, null)

  function move(from: number, to: number) {
    if (from === to || from < 0 || to < 0 || to >= order.length) return
    const next = [...order]
    next.splice(to, 0, ...next.splice(from, 1))
    startTransition(() => reorder(next))
  }

  const dragProps = (c: TestCase, i: number) =>
    manage
      ? {
          onDragOver: (e: DragEvent) => {
            if (!drag) return
            e.preventDefault()
            if (drag.over !== c.id) setDrag({ ...drag, over: c.id })
          },
          onDrop: (e: DragEvent) => {
            e.preventDefault()
            if (drag) move(order.findIndex((x) => x.id === drag.id), i)
            setDrag(null)
          },
          'data-over': drag && drag.over === c.id && drag.id !== c.id ? '' : undefined,
          'data-dragging': drag?.id === c.id ? '' : undefined,
        }
      : {}

  return (
    <div className="grid gap-2">
      <ul className="grid m-0 p-0 bg-surface border rounded-lg overflow-hidden" style={{ listStyle: 'none' }} aria-label="Test cases">
        {order.map((c, i) => (
          <li key={c.id} className="test-case" {...dragProps(c, i)}>
            <CaseRow
              workspace={workspace}
              issue={issue}
              c={c}
              manage={manage}
              onRun={onRun}
              handle={
                manage ? (
                  <button
                    type="button"
                    className="test-handle"
                    draggable
                    aria-label={`Reorder ${c.title}. Use the up and down arrow keys.`}
                    onDragStart={(e) => {
                      e.dataTransfer.effectAllowed = 'move'
                      const row = e.currentTarget.closest('li')
                      if (row) e.dataTransfer.setDragImage(row, 16, 16)
                      setDrag({ id: c.id, over: null })
                    }}
                    onDragEnd={() => setDrag(null)}
                    onKeyDown={(e) => {
                      if (e.key !== 'ArrowUp' && e.key !== 'ArrowDown') return
                      e.preventDefault()
                      move(i, i + (e.key === 'ArrowUp' ? -1 : 1))
                    }}
                  >
                    <svg width={16} height={16} viewBox="0 0 16 16" fill="currentColor" aria-hidden>
                      {[4, 8, 12].map((y) => (
                        <g key={y}>
                          <circle cx={6} cy={y} r={1.25} />
                          <circle cx={10} cy={y} r={1.25} />
                        </g>
                      ))}
                    </svg>
                  </button>
                ) : null
              }
              onSaved={(saved) => onChange(cases.map((x) => (x.id === saved.id ? saved : x)))}
              onDeleted={() => onChange(cases.filter((x) => x.id !== c.id))}
            />
          </li>
        ))}
      </ul>
      <FormError>{moveError}</FormError>
    </div>
  )
}

type RowProps = {
  workspace: Workspace
  issue: Issue
  c: TestCase
  manage: boolean
  handle: ReactNode
  onRun?: (caseId: string) => void
  onSaved: (c: TestCase) => void
  onDeleted: () => void
}

function CaseRow({ workspace, issue, c, manage, handle, onRun, onSaved, onDeleted }: RowProps) {
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState(false)

  if (editing) {
    return (
      <div className="px-6 py-4">
        <CaseForm
          workspace={workspace}
          issue={issue}
          testCase={c}
          onDone={(saved) => {
            if (saved) onSaved(saved)
            setEditing(false)
          }}
        />
      </div>
    )
  }

  const last = c.last_result
  return (
    <>
      <div className="flex items-center gap-2 px-4" style={{ minHeight: 56 }}>
        {handle}
        {last ? (
          <Link
            to="/$workspace/issue/$identifier"
            params={{ workspace: workspace.slug, identifier: issue.identifier }}
            search={{ run: last.run_id }}
            className="test-result-link"
            title={`${RESULT_LABELS[last.status]} ${relativeTime(last.at)}`}
          >
            <ResultIcon status={last.status} />
          </Link>
        ) : (
          <span className="test-result-link">
            <ResultIcon status={null} />
          </span>
        )}
        <button type="button" className="test-case-toggle flex-grow" aria-expanded={open} onClick={() => setOpen(!open)}>
          <span className="text-label text-primary">{c.title}</span>
        </button>
        {c.recorded ? (
          <span className="label-chip flex-noshrink" title="Runs replay the steps recorded when it last passed, without AI. If the page changed, AI takes over.">
            Recorded
          </span>
        ) : null}
        {onRun ? (
          <Button variant="ghost" onClick={() => onRun(c.id)} title={c.recorded ? 'Replay without AI' : 'Run with AI and record the steps'}>
            Run
          </Button>
        ) : null}
        {manage ? (
          <>
            <Button variant="ghost" onClick={() => setEditing(true)}>
              Edit
            </Button>
            <ConfirmButton
              label="Delete"
              title="Delete this test case?"
              description="Past results keep the case as it ran."
              confirm="Delete test case"
              pending="Deleting…"
              onConfirm={async () => {
                await deleteTestCase(workspace, c.id)
                onDeleted()
              }}
            />
          </>
        ) : null}
      </div>
      {open ? (
        <div className="grid gap-3 pb-4" style={{ paddingInline: manage ? 88 : 56 }}>
          <CaseText steps={c.steps} expected={c.expected} />
        </div>
      ) : null}
    </>
  )
}

export function CaseText({ steps, expected }: { steps: string[]; expected: string }) {
  return (
    <>
      <div className="grid gap-1">
        <h4 className="text-caption text-secondary m-0">Steps</h4>
        <ol className="text-body-sm m-0 grid gap-1" style={{ paddingLeft: 20 }}>
          {steps.map((s, i) => (
            <li key={i}>{s}</li>
          ))}
        </ol>
      </div>
      <div className="grid gap-1">
        <h4 className="text-caption text-secondary m-0">Expected result</h4>
        <p className="text-body-sm m-0" style={{ whiteSpace: 'pre-wrap' }}>{expected}</p>
      </div>
    </>
  )
}

type State = { errors?: FieldErrors; error?: string | null }

function CaseForm({ workspace, issue, testCase, onDone }: { workspace: Workspace; issue: Issue; testCase?: TestCase; onDone: (saved: TestCase | null) => void }) {
  const [title, setTitle] = useState(testCase?.title ?? '')
  const [steps, setSteps] = useState(testCase?.steps.join('\n') ?? '')
  const [expected, setExpected] = useState(testCase?.expected ?? '')

  const [{ errors, error }, submit, pending] = useActionState(async (): Promise<State> => {
    const lines = steps.split('\n').map((s) => s.trim()).filter(Boolean)
    const errors = { title: testCaseTitle(title), steps: testCaseSteps(lines), expected: testCaseExpected(expected) }
    if (Object.values(errors).some(Boolean)) return { errors }
    let saved
    try {
      saved = await saveTestCase(workspace, issue.identifier, { title: title.trim(), steps: lines, expected: expected.trim() }, testCase?.id)
    } catch (e) {
      return toFormErrors(e)
    }
    onDone(saved)
    return {}
  }, {})

  return (
    <form action={submit} noValidate className="grid gap-4">
      <Input label="Title" placeholder="Sign in with a valid account" autoComplete="off" autoFocus value={title} onChange={(e) => setTitle(e.target.value)} error={errors?.title} />
      <Textarea label="Steps, one per line" rows={4} placeholder={'Open the sign-in page\nEnter the email and password\nPress Sign in'} value={steps} onChange={(e) => setSteps(e.target.value)} error={errors?.steps} />
      <Textarea label="Expected result" rows={2} placeholder="The dashboard opens" value={expected} onChange={(e) => setExpected(e.target.value)} error={errors?.expected} />
      <FormError>{error}</FormError>
      <div className="flex items-center gap-3">
        <Button type="submit" disabled={pending}>
          {testCase ? (pending ? 'Saving…' : 'Save') : pending ? 'Adding…' : 'Add test case'}
        </Button>
        <Button variant="ghost" onClick={() => onDone(null)} disabled={pending}>
          Cancel
        </Button>
      </div>
    </form>
  )
}

function counts(r: TestRunSummary) {
  const c = r.counts
  if (!c) return null
  return (['passed', 'failed', 'skipped', 'error'] as const)
    .filter((k) => c[k])
    .map((k) => `${c[k]} ${RESULT_LABELS[k].toLowerCase()}`)
    .join(', ')
}

function History({ workspace, issue, runs, error }: { workspace: Workspace; issue: Issue; runs: TestRunSummary[] | null; error: string | null }) {
  if (!runs?.length && !error) return null
  return (
    <div className="grid gap-2">
      <h3 className="text-caption text-secondary m-0">Test history</h3>
      <FormError>{error}</FormError>
      <ul className="grid m-0 p-0 bg-surface border rounded-lg overflow-hidden" style={{ listStyle: 'none' }} aria-label="Test history">
        {runs?.map((r) => (
          <li key={r.id}>
            <Link
              to="/$workspace/issue/$identifier"
              params={{ workspace: workspace.slug, identifier: issue.identifier }}
              search={{ run: r.id }}
              className="issue-row flex-wrap"
            >
              <RunStatusPill status={r.status} />
              <span className="text-label text-primary flex-noshrink">{r.kind === 'ai' ? 'AI test' : 'Manual run'}</span>
              <span className="text-caption text-secondary ellipsis flex-grow">{counts(r)}</span>
              {r.kind === 'ai' && r.cost_usd != null ? <span className="text-caption text-secondary flex-noshrink">{formatCost(r.cost_usd)}</span> : null}
              {r.started_by ? <span className="issue-row-wide text-caption text-secondary flex-noshrink">{r.started_by.name}</span> : null}
              <time dateTime={r.created_at} title={fullTime(r.created_at)} className="text-caption text-secondary flex-noshrink">
                {relativeTime(r.created_at)}
              </time>
            </Link>
          </li>
        ))}
      </ul>
    </div>
  )
}
