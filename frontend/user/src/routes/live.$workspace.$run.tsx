import { createFileRoute } from '@tanstack/react-router'
import { apiUrl } from '@taskwell/shared/api-url'
import { useEffect, useState } from 'react'
import type { TestRun } from '@/api'
import { RunStatusPill, formatCost, runFinished, useLive } from '@/components/tests/tests'
import { useRealtime } from '@/realtime'
import { workspacePath } from '@/api'

// Outside _app so the popup has no navbar or sidebar; the API answers 401 when signed out.
export const Route = createFileRoute('/live/$workspace/$run')({
  component: LiveView,
})

function LiveView() {
  const { workspace, run: runId } = Route.useParams()
  useRealtime(workspace)
  const path = `${workspacePath(workspace)}/test-runs/${encodeURIComponent(runId)}`
  const { data: run, error } = useLive<TestRun>(path)
  const [attempt, setAttempt] = useState(0)
  const [broken, setBroken] = useState(false)
  const streaming = run?.kind !== 'manual' && run?.status === 'running'
  const current = run?.results.find((r) => r.status === 'running')
  const done = run?.results.filter((r) => !['pending', 'running'].includes(r.status)).length ?? 0

  useEffect(() => {
    if (run) document.title = `Live: ${run.issue.identifier} · Taskwell`
  }, [run])
  useEffect(() => {
    if (!broken || !streaming) return
    const t = setTimeout(() => {
      setBroken(false)
      setAttempt((a) => a + 1)
    }, 2000)
    return () => clearTimeout(t)
  }, [broken, streaming])

  const message = error
    ? error
    : !run
      ? 'Loading…'
      : run.kind === 'manual'
        ? 'Manual runs have no live browser.'
        : run.status === 'queued'
          ? 'Waiting for a test runner…'
          : run.status === 'generating'
            ? 'Writing test cases…'
            : runFinished(run.status)
              ? 'This run has finished.'
              : broken
                ? 'Reconnecting…'
                : null

  return (
    <div className="grid h-screen" style={{ gridTemplateRows: 'auto minmax(0, 1fr)', background: 'var(--bg)' }}>
      <header className="flex items-center gap-3 px-4 py-3" style={{ borderBottom: '1px solid var(--border)' }}>
        <span className="text-label text-primary ellipsis flex-grow">
          {run ? `${run.issue.identifier} · ${current?.title ?? run.issue.title}` : 'Live test run'}
        </span>
        {run?.results.length ? <span className="text-caption text-secondary flex-noshrink">{`${done} of ${run.results.length} cases`}</span> : null}
        {run?.cost_usd != null ? <span className="text-caption text-secondary flex-noshrink">{formatCost(run.cost_usd)}</span> : null}
        {run ? <RunStatusPill status={run.status} /> : null}
      </header>
      <main className="grid p-4" style={{ placeItems: 'center', minHeight: 0 }}>
        {streaming && !broken ? (
          <img
            key={attempt}
            src={apiUrl(`/api${path}/live`)}
            alt="Live view of the test browser"
            onError={() => setBroken(true)}
            style={{ maxWidth: '100%', maxHeight: '100%', objectFit: 'contain', borderRadius: 8, border: '1px solid var(--border)' }}
          />
        ) : run && runFinished(run.status) && run.latest_screenshot_url ? (
          <div className="grid gap-3" style={{ placeItems: 'center', maxHeight: '100%', minHeight: 0 }}>
            <p className="text-body-sm text-secondary m-0">{message}</p>
            <img
              src={apiUrl(run.latest_screenshot_url)}
              alt="Last screenshot"
              style={{ maxWidth: '100%', maxHeight: 'calc(100vh - 140px)', objectFit: 'contain', borderRadius: 8, border: '1px solid var(--border)' }}
            />
          </div>
        ) : (
          <p className="text-body-sm text-secondary m-0" role="status">
            {message}
          </p>
        )}
      </main>
    </div>
  )
}
