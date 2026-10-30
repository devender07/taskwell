import { useRouter } from '@tanstack/react-router'
import { startTransition, useActionState, useOptimistic, useState } from 'react'
import { Button } from '@tether/shared/button'
import { Dialog } from '@tether/shared/dialog'
import { IconButton } from '@tether/shared/icon-button'
import { Input } from '@tether/shared/input'
import { SmartAutocomplete } from '@tether/shared/smart-autocomplete'
import { statusCategory, statusName } from '@tether/shared/validate'
import { api, toFormErrors, type FieldErrors, type Status, type Workspace } from '@/api'
import { FormError } from '@/components/auth/auth-card'
import { StatusIcon, categoryLabel, categoryOptions, statusOptions } from '@/components/issues/fields'

type State = { errors?: FieldErrors; error?: string | null }

const statusesPath = (workspace: Workspace) => `/workspaces/${encodeURIComponent(workspace.slug)}/statuses`

// Create (no status) or rename/recategorize a status.
function StatusForm({ workspace, status, onDone }: { workspace: Workspace; status?: Status; onDone?: () => void }) {
  const router = useRouter()
  const [name, setName] = useState(status?.name ?? '')
  const [category, setCategory] = useState<string>(status?.category ?? 'unstarted')

  const [{ errors, error }, submit, pending] = useActionState(async (): Promise<State> => {
    const errors = { name: statusName(name), category: statusCategory(category) }
    if (Object.values(errors).some(Boolean)) return { errors }
    const form = new FormData()
    form.set('name', name.trim())
    form.set('category', category)
    try {
      await api<Status>(status ? `${statusesPath(workspace)}/${encodeURIComponent(status.id)}` : statusesPath(workspace), form)
    } catch (e) {
      return toFormErrors(e)
    }
    await router.invalidate()
    if (!status) setName('')
    onDone?.()
    return {}
  }, {})

  return (
    <form action={submit} noValidate className="grid gap-2">
      <div className="flex flex-wrap items-start gap-3">
        <div className="flex-grow" style={{ minWidth: 160 }}>
          <Input aria-label="Status name" placeholder="In review" autoComplete="off" autoFocus={Boolean(status)} value={name} onChange={(e) => setName(e.target.value)} error={errors?.name} />
        </div>
        <div className="flex-noshrink" style={{ width: 160 }}>
          <SmartAutocomplete
            aria-label="Category"
            options={categoryOptions}
            value={categoryOptions.find((o) => o.value === category) ?? null}
            onChange={(o) => setCategory(o?.value ?? '')}
            error={errors?.category}
          />
        </div>
        <Button type="submit" disabled={pending}>
          {status ? (pending ? 'Saving…' : 'Save') : pending ? 'Adding…' : 'Add status'}
        </Button>
        {status ? (
          <Button variant="ghost" onClick={onDone} disabled={pending}>
            Cancel
          </Button>
        ) : null}
      </div>
      <FormError>{error}</FormError>
    </form>
  )
}

export function AddStatus({ workspace }: { workspace: Workspace }) {
  return (
    <section className="grid gap-4 p-6 bg-surface border rounded-lg">
      <h2 className="text-label">Add a status</h2>
      <StatusForm workspace={workspace} />
    </section>
  )
}

export function StatusList({ workspace, statuses, manage }: { workspace: Workspace; statuses: Status[]; manage: boolean }) {
  const router = useRouter()
  const [order, setOrder] = useOptimistic(statuses)

  const [moveError, reorder] = useActionState(async (_prev: string | null, next: Status[]) => {
    setOrder(next)
    const form = new FormData()
    for (const s of next) form.append('ids', s.id)
    try {
      await api<Status[]>(`${statusesPath(workspace)}/order`, form)
    } catch (e) {
      return toFormErrors(e).error
    }
    await router.invalidate()
    return null
  }, null)

  function move(i: number, dir: -1 | 1) {
    const next = [...order]
    ;[next[i], next[i + dir]] = [next[i + dir]!, next[i]!]
    startTransition(() => reorder(next))
  }

  return (
    <section className="grid gap-2">
      <ul className="grid m-0 p-0 bg-surface border rounded-lg" style={{ listStyle: 'none' }} aria-label="Statuses">
        {order.map((s, i) => (
          <StatusRow
            key={s.id}
            workspace={workspace}
            status={s}
            statuses={order}
            manage={manage}
            onUp={i > 0 ? () => move(i, -1) : undefined}
            onDown={i < order.length - 1 ? () => move(i, 1) : undefined}
          />
        ))}
      </ul>
      <FormError>{moveError}</FormError>
    </section>
  )
}

type RowProps = { workspace: Workspace; status: Status; statuses: Status[]; manage: boolean; onUp?: () => void; onDown?: () => void }

function StatusRow({ workspace, status, statuses, manage, onUp, onDown }: RowProps) {
  const [editing, setEditing] = useState(false)
  const [deleting, setDeleting] = useState(false)

  if (editing) {
    return (
      <li className="px-6 py-3">
        <StatusForm workspace={workspace} status={status} onDone={() => setEditing(false)} />
      </li>
    )
  }

  return (
    <li className="flex flex-wrap items-center gap-3 px-6 py-2" style={{ minHeight: 56 }}>
      <StatusIcon category={status.category} />
      <div className="grid flex-grow" style={{ minWidth: 0 }}>
        <span className="text-label text-primary ellipsis">{status.name}</span>
        <span className="text-caption text-secondary">{categoryLabel(status.category)}</span>
      </div>
      {manage ? (
        <>
          <IconButton aria-label={`Move ${status.name} up`} disabled={!onUp} onClick={onUp}>
            <Chevron d="m6 15 6-6 6 6" />
          </IconButton>
          <IconButton aria-label={`Move ${status.name} down`} disabled={!onDown} onClick={onDown}>
            <Chevron d="m6 9 6 6 6-6" />
          </IconButton>
          <Button variant="ghost" onClick={() => setEditing(true)}>
            Edit
          </Button>
          {statuses.length > 1 ? (
            <Button variant="ghost" onClick={() => setDeleting(true)}>
              Delete
            </Button>
          ) : null}
          <Dialog open={deleting} onClose={() => setDeleting(false)} title={`Delete ${status.name}?`} description="Issues with this status move to the one you pick.">
            <DeleteStatusForm workspace={workspace} status={status} statuses={statuses} onDone={() => setDeleting(false)} />
          </Dialog>
        </>
      ) : null}
    </li>
  )
}

function DeleteStatusForm({ workspace, status, statuses, onDone }: { workspace: Workspace; status: Status; statuses: Status[]; onDone: () => void }) {
  const router = useRouter()
  const options = statusOptions(statuses.filter((s) => s.id !== status.id))
  const [moveTo, setMoveTo] = useState(options[0] ?? null)

  const [error, remove, pending] = useActionState(async () => {
    if (!moveTo) return 'Choose a status.'
    try {
      await api(`${statusesPath(workspace)}/${encodeURIComponent(status.id)}?move_to=${encodeURIComponent(moveTo.value)}`, undefined, 'DELETE')
    } catch (e) {
      return toFormErrors(e).error
    }
    await router.invalidate()
    onDone()
    return null
  }, null)

  return (
    <form action={remove} className="grid gap-4">
      <SmartAutocomplete label="Move issues to" options={options} value={moveTo} onChange={setMoveTo} />
      <FormError>{error}</FormError>
      <Button type="submit" variant="danger" className="w-full" disabled={pending}>
        {pending ? 'Deleting…' : 'Delete status'}
      </Button>
    </form>
  )
}

function Chevron({ d }: { d: string }) {
  return (
    <svg width={20} height={20} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.75} strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <path d={d} />
    </svg>
  )
}
