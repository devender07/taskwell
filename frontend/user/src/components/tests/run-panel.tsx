import { startTransition, useActionState, useEffect, useEffectEvent, useState } from 'react'
import { apiUrl } from '@tether/shared/api-url'
import { Button } from '@tether/shared/button'
import { IconButton } from '@tether/shared/icon-button'
import { Textarea } from '@tether/shared/textarea'
import { fullTime, relativeTime } from '@tether/shared/time'
import { testNote } from '@tether/shared/validate'
import { XIcon } from '@tether/shared/icons'
import {
  can,
  cancelTestRun,
  markTestResult,
  testCasesPath,
  testRunPath,
  testRunsPath,
  toFormErrors,
  type TestResult,
  type RunKind,
  type TestRun,
  type Workspace,
} from '@/api'
import { FormError } from '@/components/auth/auth-card'
import { singleKeyShortcuts, typing, useShortcuts } from '@/components/app/shortcuts'
import { refetchPaths } from '@/realtime'
import { CaseText, ResultIcon, RunStatusPill, bugPrefill, formatCost, runFinished, useLive } from './tests'
import './tests.css'

type Mark = 'passed' | 'failed' | 'skipped'
const KEYS: Record<string, Mark> = { p: 'passed', f: 'failed', s: 'skipped' }

const nextPending = (run: TestRun, after: string) => {
  const i = run.results.findIndex((r) => r.id === after)
  return [...run.results.slice(i + 1), ...run.results.slice(0, i)].find((r) => r.status === 'pending')
}

const RUN_TITLES: Record<RunKind, string> = { ai: 'AI test run', manual: 'Manual test run', replay: 'Recorded test run' }

// Side panel for one run (?run=<id>). P / F / S mark the selected case of a running manual run; while the panel is
// open they win over the issue page's S and P, and A and L keep working.
export function RunPanel({ workspace, identifier, runId, onClose }: { workspace: Workspace; identifier: string; runId: string; onClose: () => void }) {
  const live = useLive<TestRun>(testRunPath(workspace, runId))
  const run = live.data
  const [selected, setSelected] = useState<string | null>(null)
  const [notes, setNotes] = useState<Record<string, string>>({})
  const { createPrefilled } = useShortcuts()

  const result =
    run?.results.find((r) => r.id === selected) ?? run?.results.find((r) => r.status === 'running' || r.status === 'pending') ?? run?.results[0]
  const finished = run ? runFinished(run.status) : true
  const markable = run?.kind === 'manual' && !finished && can(workspace, 'tests.manage') && Boolean(result)
  const cancellable = run && !finished && can(workspace, run.kind === 'ai' ? 'ai_tests.run' : 'tests.manage')
  const note = result ? (notes[result.id] ?? result.summary ?? '') : ''
  const changed = [testRunsPath(workspace, identifier), testCasesPath(workspace, identifier)]

  const [markError, mark, marking] = useActionState(async (_prev: string | null, status: Mark) => {
    if (!result) return null
    const invalid = testNote(note)
    if (invalid) return invalid
    let next
    try {
      next = await markTestResult(workspace, runId, result.id, status, note.trim())
    } catch (e) {
      return toFormErrors(e).error
    }
    live.set(next)
    refetchPaths(changed)
    const after = nextPending(next, result.id)
    if (after) setSelected(after.id)
    return null
  }, null)

  const [cancelError, cancel, cancelling] = useActionState(async () => {
    try {
      live.set(await cancelTestRun(workspace, runId))
    } catch (e) {
      return toFormErrors(e).error
    }
    refetchPaths(changed)
    return null
  }, null)

  const onKey = useEffectEvent((e: KeyboardEvent) => {
    if (e.defaultPrevented || e.isComposing || e.metaKey || e.ctrlKey || e.altKey || typing(e.target) || document.querySelector('dialog[open]')) return
    if (e.key === 'Escape') {
      if (document.querySelector('.popover')) return
      e.preventDefault()
      onClose()
      return
    }
    const status = KEYS[e.key]
    if (!status || !markable || marking || !singleKeyShortcuts()) return
    e.preventDefault()
    startTransition(() => mark(status))
  })

  useEffect(() => {
    window.addEventListener('keydown', onKey, true)
    return () => window.removeEventListener('keydown', onKey, true)
  }, [])

  return (
    <aside className="run-panel" aria-label="Test run">
      <header className="flex flex-wrap items-center gap-3 px-6 py-4" style={{ borderBottom: '1px solid var(--border)' }}>
        <h2 className="text-label flex-grow">{run ? RUN_TITLES[run.kind] : 'Test run'}</h2>
        {run ? <RunStatusPill status={run.status} /> : null}
        {run?.kind === 'ai' && run.cost_usd != null ? <span className="text-caption text-secondary">{formatCost(run.cost_usd)}</span> : null}
        {run && run.kind !== 'manual' && !finished ? <WatchLive workspace={workspace.slug} runId={runId} /> : null}
        {cancellable ? (
          <Button variant="ghost" disabled={cancelling} onClick={() => startTransition(cancel)}>
            {cancelling ? 'Cancelling…' : 'Cancel run'}
          </Button>
        ) : null}
        <IconButton aria-label="Close test run" onClick={onClose}>
          <XIcon size={20} strokeWidth={2} />
        </IconButton>
      </header>
      <div className="grid gap-6 p-6 overflow-y-auto" style={{ alignContent: 'start', gridAutoRows: 'max-content' }}>
        <FormError>{live.error ?? cancelError}</FormError>
        {!run ? (
          live.error ? null : <p className="text-body-sm text-secondary">Loading…</p>
        ) : (
          <>
            <p className="text-caption text-secondary m-0">
              {run.started_by ? `Started by ${run.started_by.name} ` : 'Started '}
              <time dateTime={run.created_at} title={fullTime(run.created_at)}>{relativeTime(run.created_at)}</time>
            </p>
            {run.error ? <p className="text-body-sm text-accent m-0" role="alert">{run.error}</p> : null}
            {run.results.length === 0 ? (
              <p className="text-body-sm text-secondary m-0">{run.status === 'generating' ? 'Writing test cases…' : run.status === 'queued' ? 'Waiting for a test runner…' : 'No results.'}</p>
            ) : (
              <ul className="grid m-0 p-0 border rounded-lg overflow-hidden" style={{ listStyle: 'none' }} aria-label="Results">
                {run.results.map((r) => (
                  <li key={r.id}>
                    <button type="button" className="test-result-item" aria-current={r.id === result?.id ? 'true' : undefined} onClick={() => setSelected(r.id)}>
                      <ResultIcon status={r.status} />
                      <span className="text-label text-primary ellipsis flex-grow">{r.title}</span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
            {result ? (
              <ResultDetail
                key={result.id}
                run={run}
                result={result}
                note={note}
                onNote={(v) => setNotes({ ...notes, [result.id]: v })}
                markable={markable}
                marking={marking}
                markError={markError}
                onMark={(s) => startTransition(() => mark(s))}
                onBug={can(workspace, 'issues.create') && result.status === 'failed' ? () => createPrefilled(bugPrefill(identifier, result)) : undefined}
              />
            ) : null}
          </>
        )}
      </div>
    </aside>
  )
}

type DetailProps = {
  run: TestRun
  result: TestResult
  note: string
  onNote: (v: string) => void
  markable: boolean
  marking: boolean
  markError: string | null
  onMark: (s: Mark) => void
  onBug?: () => void
}

function ResultDetail({ run, result, note, onNote, markable, marking, markError, onMark, onBug }: DetailProps) {
  const [picked, setPicked] = useState<number | null>(null)
  const log = result.log ?? []
  const pickedShot = log.find((s) => s.position === picked)?.screenshot_url
  const lastShot = [...log].reverse().find((s) => s.screenshot_url)?.screenshot_url
  const shot = pickedShot ?? lastShot ?? (result.status === 'running' ? run.latest_screenshot_url : null)

  return (
    <section className="grid gap-4" aria-labelledby="run-result-heading">
      <div className="flex items-start justify-between gap-3">
        <h3 id="run-result-heading" className="text-label m-0" style={{ overflowWrap: 'anywhere' }}>{result.title}</h3>
        {onBug ? (
          <Button variant="secondary" className="flex-noshrink" onClick={onBug}>
            Create bug
          </Button>
        ) : null}
      </div>
      <CaseText steps={result.steps} expected={result.expected} />
      {result.summary && !markable ? (
        <div className="grid gap-1">
          <h4 className="text-caption text-secondary m-0">{run.kind === 'manual' ? 'Note' : 'Summary'}</h4>
          <p className="text-body-sm m-0" style={{ whiteSpace: 'pre-wrap' }}>{result.summary}</p>
        </div>
      ) : null}
      {result.marked_by ? (
        <p className="text-caption text-secondary m-0">
          Marked by {result.marked_by.name}
          {result.marked_at ? <> {relativeTime(result.marked_at)}</> : null}
        </p>
      ) : null}
      {result.reproduction?.length ? (
        <div className="grid gap-1">
          <h4 className="text-caption text-secondary m-0">Reproduction</h4>
          <ol className="text-body-sm m-0 grid gap-1" style={{ paddingLeft: 20 }}>
            {result.reproduction.map((s, i) => (
              <li key={i}>{s}</li>
            ))}
          </ol>
        </div>
      ) : null}
      {markable ? (
        <div className="grid gap-3">
          <Textarea label="Note" rows={2} placeholder="What you saw (optional)" value={note} onChange={(e) => onNote(e.target.value)} />
          <FormError>{markError}</FormError>
          <div className="flex flex-wrap items-center gap-2">
            <Button disabled={marking} onClick={() => onMark('passed')} aria-keyshortcuts="P">
              Passed
            </Button>
            <Button disabled={marking} onClick={() => onMark('failed')} aria-keyshortcuts="F">
              Failed
            </Button>
            <Button variant="ghost" disabled={marking} onClick={() => onMark('skipped')} aria-keyshortcuts="S">
              Skip
            </Button>
            <span className="text-caption text-secondary">
              <kbd className="kbd">P</kbd> <kbd className="kbd">F</kbd> <kbd className="kbd">S</kbd>
            </span>
          </div>
        </div>
      ) : null}
      {shot ? <img src={apiUrl(shot)} alt={picked !== null && pickedShot ? `Screenshot after step ${picked}` : 'Latest screenshot'} className="test-screenshot" /> : null}
      {log.length ? (
        <div className="grid gap-1">
          <h4 className="text-caption text-secondary m-0">Step log</h4>
          <ol className="grid m-0 p-0 border rounded-lg overflow-hidden" style={{ listStyle: 'none' }}>
            {log.map((s) => (
              <li key={s.position}>
                <button
                  type="button"
                  className="test-log-row"
                  aria-pressed={picked === s.position}
                  disabled={!s.screenshot_url}
                  onClick={() => setPicked(picked === s.position ? null : s.position)}
                >
                  <span className="issue-id flex-noshrink">{s.position}</span>
                  <span className="grid" style={{ minWidth: 0 }}>
                    <span className="text-caption text-primary" style={{ overflowWrap: 'anywhere' }}>{s.action}</span>
                    {s.url ? <span className="text-caption text-secondary ellipsis">{s.url}</span> : null}
                  </span>
                </button>
              </li>
            ))}
          </ol>
        </div>
      ) : null}
    </section>
  )
}

// Opens the live browser view in a popup window; with popups blocked, the link opens it in a new tab instead.
function WatchLive({ workspace, runId }: { workspace: string; runId: string }) {
  const href = `/live/${encodeURIComponent(workspace)}/${encodeURIComponent(runId)}`
  const name = `tether-live-${runId}`
  return (
    <a
      href={href}
      target={name}
      className="button button-secondary"
      onClick={(e) => {
        if (window.open(href, name, 'popup,width=1320,height=900')) e.preventDefault()
      }}
    >
      Watch live
    </a>
  )
}
