import { useRouter } from '@tanstack/react-router'
import { startTransition, useActionState, useMemo, useState, type ReactNode } from 'react'
import { Button } from '@taskwell/shared/button'
import { Dialog } from '@taskwell/shared/dialog'
import { Popover } from '@taskwell/shared/popover'
import { SmartAutocomplete } from '@taskwell/shared/smart-autocomplete'
import { api, bulkIssuesPath, toFormErrors, type Issue, type Workspace } from '@/api'
import { reloadPage } from '@/realtime'
import { FormError } from '@/components/auth/auth-card'
import { peopleSearch } from '@/components/workspace/member-pages'
import { loadAssignees } from './fields'
import type { Choice } from './issue-view'

type Change = { ids: string[]; fields: [string, string][] }
type Props = { workspace: Workspace; issues: Issue[]; choices: Choice[]; onClear: () => void; onDone: (message: string) => void }

const count = (n: number) => `${n} ${n === 1 ? 'issue' : 'issues'}`

// Bar along the bottom of the window while list rows are selected: each action changes every selected issue in one
// request. A failure keeps the selection so it can be retried.
export function BulkBar({ workspace, issues, choices, onClear, onDone }: Props) {
  const router = useRouter()
  const [confirming, setConfirming] = useState(false)
  const people = useMemo(() => loadAssignees(peopleSearch(workspace.slug)), [workspace.slug])
  const [error, run, pending] = useActionState(async (_prev: string | null, { ids, fields }: Change) => {
    const form = new FormData()
    for (const id of ids) form.append('ids', id)
    for (const [k, v] of fields) form.append(k, v)
    let res: { updated?: number; deleted?: number }
    try {
      res = await api<{ updated?: number; deleted?: number }>(bulkIssuesPath(workspace.slug), form)
    } catch (e) {
      const { errors, error } = toFormErrors(e)
      return Object.values(errors).find(Boolean) ?? error
    }
    setConfirming(false)
    await reloadPage(router)
    onDone(res.deleted !== undefined ? `Deleted ${count(res.deleted)}.` : `Updated ${count(res.updated ?? ids.length)}.`)
    return null
  }, null)
  const send = (...fields: [string, string][]) => startTransition(() => run({ ids: issues.map((i) => i.identifier), fields }))
  const options = (key: Choice['key']) => choices.find((c) => c.key === key)?.options ?? []
  const cycles = options('cycle')
  const labels = options('label')

  const menu = (label: string, field: string, list: Choice['options']) => (
    <Menu label={label}>
      {(close) =>
        list.map((o) => (
          <Item key={o.value} icon={o.icon} onClick={() => (close(), send([field, o.value === 'none' ? '' : o.value]))}>
            {o.label}
          </Item>
        ))
      }
    </Menu>
  )

  return (
    <div className="bulk-bar" role="region" aria-label="Selected issues">
      <span className="text-label" style={{ paddingInline: 8 }}>{issues.length} selected</span>
      <Button variant="ghost" onClick={onClear}>
        Clear
      </Button>
      {menu('Status', 'status_id', options('status'))}
      <Menu label="Assignee">
        {(close) => (
          <div style={{ width: 240 }}>
            <SmartAutocomplete
              aria-label="Assignee"
              placeholder="Search people"
              loadOptions={people}
              value={null}
              onChange={(o) => o && (close(), send(['assignee_id', o.value]))}
            />
          </div>
        )}
      </Menu>
      {menu('Priority', 'priority', options('priority'))}
      {labels.length ? (
        <Menu label="Labels">
          {(close) =>
            (['add', 'remove'] as const).map((op) => (
              <div key={op} role="group" aria-label={op === 'add' ? 'Add label' : 'Remove label'} className="grid">
                <span className="text-label text-secondary px-3 py-2">{op === 'add' ? 'Add label' : 'Remove label'}</span>
                {labels.map((o) => (
                  <Item key={o.value} icon={o.icon} onClick={() => (close(), send([`${op}_label_ids`, o.value]))}>
                    {o.label}
                  </Item>
                ))}
              </div>
            ))
          }
        </Menu>
      ) : null}
      {cycles.length ? menu('Cycle', 'cycle', cycles) : null}
      <Button variant="ghost" onClick={() => setConfirming(true)}>
        Delete
      </Button>
      {pending ? <span className="text-caption text-secondary">Saving…</span> : null}
      {error && !confirming ? (
        <div style={{ flexBasis: '100%', paddingInline: 8 }}>
          <FormError>{error}</FormError>
        </div>
      ) : null}
      <Dialog open={confirming} onClose={() => setConfirming(false)} title={`Delete ${count(issues.length)}?`} description="This can't be undone.">
        <form action={() => send(['delete', '1'])} className="grid gap-4">
          <FormError>{error}</FormError>
          <Button type="submit" variant="danger" className="w-full" disabled={pending}>
            {pending ? 'Deleting…' : 'Delete'}
          </Button>
        </form>
      </Dialog>
    </div>
  )
}

function Menu({ label, children }: { label: string; children: (close: () => void) => ReactNode }) {
  return (
    <Popover
      aria-label={label}
      placement="top-start"
      trigger={(props) => (
        <Button variant="secondary" {...props}>
          {label}
        </Button>
      )}
    >
      {(close) => (
        <div className="grid p-1" style={{ minWidth: 200, maxHeight: 360, overflowY: 'auto' }}>
          {children(close)}
        </div>
      )}
    </Popover>
  )
}

function Item({ icon, onClick, children }: { icon?: ReactNode; onClick: () => void; children: ReactNode }) {
  return (
    <button type="button" className="popover-item" style={{ gap: 8 }} onClick={onClick}>
      {icon}
      <span className="ellipsis">{children}</span>
    </button>
  )
}
