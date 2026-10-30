import { useRouter } from '@tanstack/react-router'
import { useOptimistic, useState, useTransition } from 'react'
import { Button } from '@tether/shared/button'
import { api, toFormErrors, type Issue, type Workspace } from '@/api'

export function WatchButton({ workspace, issue }: { workspace: Workspace; issue: Issue }) {
  const router = useRouter()
  const [watching, setWatching] = useOptimistic(issue.watching)
  const [error, setError] = useState<string | null>(null)
  const [, startTransition] = useTransition()

  function toggle() {
    const next = !watching
    setError(null)
    startTransition(async () => {
      setWatching(next)
      try {
        await api(`/workspaces/${encodeURIComponent(workspace.slug)}/issues/${encodeURIComponent(issue.identifier)}/watch`, undefined, next ? 'POST' : 'DELETE')
        await router.invalidate()
      } catch (e) {
        setError(toFormErrors(e).error)
      }
    })
  }

  return (
    <div className="grid gap-1 flex-noshrink" style={{ justifyItems: 'end' }}>
      <Button variant="secondary" aria-pressed={watching} onClick={toggle} aria-label="Watch">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
          <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z" />
          <circle cx="12" cy="12" r="3" fill={watching ? 'currentColor' : 'none'} />
        </svg>
        {watching ? 'Watching' : 'Watch'}
      </Button>
      {error ? <p role="alert" className="m-0 text-caption text-accent">{error}</p> : null}
    </div>
  )
}
