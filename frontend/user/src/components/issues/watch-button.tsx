import { useRouter } from '@tanstack/react-router'
import { useOptimistic, useState, useTransition } from 'react'
import { Button } from '@tether/shared/button'
import { EyeIcon } from '@tether/shared/icons'
import { api, toFormErrors, type Issue, type Workspace, workspacePath } from '@/api'
import { reloadPage } from '@/realtime'

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
        await api(`${workspacePath(workspace.slug)}/issues/${encodeURIComponent(issue.identifier)}/watch`, undefined, next ? 'POST' : 'DELETE')
        await reloadPage(router)
      } catch (e) {
        setError(toFormErrors(e).error)
      }
    })
  }

  return (
    <div className="grid gap-1 flex-noshrink" style={{ justifyItems: 'end' }}>
      <Button variant="secondary" aria-pressed={watching} onClick={toggle} aria-label="Watch">
        <EyeIcon size={16} filled={watching} />
        {watching ? 'Watching' : 'Watch'}
      </Button>
      {error ? <p role="alert" className="m-0 text-caption text-accent">{error}</p> : null}
    </div>
  )
}
