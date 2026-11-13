import { useRouter } from '@tanstack/react-router'
import { useOptimistic, useState, useTransition } from 'react'
import { Avatar } from '@taskwell/shared/avatar'
import { Button } from '@taskwell/shared/button'
import { EyeIcon } from '@taskwell/shared/icons'
import { Popover } from '@taskwell/shared/popover'
import { api, avatarSrc, toFormErrors, type Issue, type PersonRef, type Workspace, workspacePath } from '@/api'
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
      <div className="flex items-center gap-1">
        <Watchers watchers={issue.watchers ?? []} />
        <Button variant="secondary" aria-pressed={watching} onClick={toggle} aria-label="Watch">
          <EyeIcon size={16} filled={watching} />
          {watching ? 'Watching' : 'Watch'}
        </Button>
      </div>
      {error ? <p role="alert" className="m-0 text-caption text-accent">{error}</p> : null}
    </div>
  )
}

const SHOWN = 3

function Watchers({ watchers }: { watchers: PersonRef[] }) {
  if (watchers.length === 0) return null
  return (
    <Popover
      aria-label="Watchers"
      placement="bottom-end"
      trigger={(props) => (
        <Button variant="ghost" aria-label={`Watchers: ${watchers.length}`} {...props}>
          <span className="flex items-center" style={{ paddingLeft: 4 }}>
            {watchers.slice(0, SHOWN).map((w) => (
              <span key={w.id} className="inline-grid rounded-full" style={{ marginLeft: -4, boxShadow: '0 0 0 2px var(--bg)' }}>
                <Avatar name={w.name} size={24} src={avatarSrc(w.id)} />
              </span>
            ))}
          </span>
          {watchers.length}
        </Button>
      )}
    >
      <div style={{ width: 240 }}>
        <p className="popover-title m-0">Watchers</p>
        <ul className="grid gap-1 m-0 p-0 pb-2" style={{ listStyle: 'none', maxHeight: 320, overflowY: 'auto' }}>
          {watchers.map((w) => (
            <li key={w.id} className="flex items-center gap-2 px-3" style={{ minHeight: 36 }}>
              <Avatar name={w.name} size={24} src={avatarSrc(w.id)} />
              <span className="ellipsis">{w.name}</span>
            </li>
          ))}
        </ul>
      </div>
    </Popover>
  )
}
