import { useRouter } from '@tanstack/react-router'
import { startTransition, useActionState, useOptimistic, useState } from 'react'
import { Button } from '@tether/shared/button'
import { Dialog } from '@tether/shared/dialog'
import { IconButton } from '@tether/shared/icon-button'
import { Input } from '@tether/shared/input'
import { SmartAutocomplete } from '@tether/shared/smart-autocomplete'
import { ColorPicker } from '@tether/shared/color-picker'
import { STATUS_ICONS, StatusTypeIcon, ChevronDownIcon, ChevronUpIcon } from '@tether/shared/icons'
import type { Option } from '@tether/shared/smart-autocomplete'
import { labelColor, statusCategory, statusName, statusTypeName } from '@tether/shared/validate'
import { api, deleteStatusType, saveStatusType, toFormErrors, type FieldErrors, type Status, type StatusCategory, type StatusType, type Workspace } from '@/api'
import { reloadPage } from '@/realtime'
import { FormError } from '@/components/auth/auth-card'
import { CATEGORIES, StatusIcon, categoryLabel, statusOptions } from '@/components/issues/fields'
import './workflow.css'

type State = { errors?: FieldErrors; error?: string | null }

const statusesPath = (workspace: Workspace) => `/workspaces/${encodeURIComponent(workspace.slug)}/statuses`

const CATEGORY_HEX: Record<StatusCategory, string> = { backlog: '#8a8f98', unstarted: '#8a8f98', started: '#f7f8f8', completed: '#00ba7c', canceled: '#8a8f98' }

const iconName = (key: string) => key.charAt(0).toUpperCase() + key.slice(1).replace('-', ' ')

function typeOptions(types: StatusType[]): Option[] {
  return [
    ...CATEGORIES.map((c) => ({ value: `category:${c.value}`, label: c.label, icon: <StatusIcon status={{ category: c.value }} /> })),
    ...types.map((t) => ({ value: `type:${t.id}`, label: t.name, hint: `Counts as ${categoryLabel(t.category)}`, icon: <StatusIcon status={t} /> })),
  ]
}

function StatusForm({ workspace, status, types, onDone }: { workspace: Workspace; status?: Status; types: StatusType[]; onDone?: () => void }) {
  const router = useRouter()
  const [name, setName] = useState(status?.name ?? '')
  const [choice, setChoice] = useState(status?.type ? `type:${status.type.id}` : `category:${status?.category ?? 'unstarted'}`)
  const options = typeOptions(types)

  const [{ errors, error }, submit, pending] = useActionState(async (): Promise<State> => {
    const [kind, ref = ''] = choice.split(':')
    const type = kind === 'type' ? types.find((t) => t.id === ref) : undefined
    const category = type?.category ?? ref
    const errors = { name: statusName(name), type_id: kind === 'type' && !type ? 'Choose a type.' : statusCategory(category) }
    if (Object.values(errors).some(Boolean)) return { errors }
    const form = new FormData()
    form.set('name', name.trim())
    form.set('category', category)
    form.set('type_id', type?.id ?? '')
    try {
      await api<Status>(status ? `${statusesPath(workspace)}/${encodeURIComponent(status.id)}` : statusesPath(workspace), form)
    } catch (e) {
      return toFormErrors(e)
    }
    await reloadPage(router)
    if (!status) setName('')
    onDone?.()
    return {}
  }, {})

  return (
    <form action={submit} noValidate className="grid gap-2">
      <div className="flex flex-wrap items-start gap-3">
        <div className="flex-grow" style={{ minWidth: 160 }}>
          <Input aria-label="Status name" placeholder="Code review" autoComplete="off" autoFocus={Boolean(status)} value={name} onChange={(e) => setName(e.target.value)} error={errors?.name} />
        </div>
        <div className="flex-noshrink" style={{ width: 200 }}>
          <SmartAutocomplete
            aria-label="Type"
            placeholder="Pick a type"
            options={options}
            value={options.find((o) => o.value === choice) ?? null}
            onChange={(o) => o && setChoice(o.value)}
            error={errors?.type_id ?? errors?.category}
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

export function AddStatus({ workspace, types }: { workspace: Workspace; types: StatusType[] }) {
  return (
    <section className="grid gap-4 p-6 bg-surface border rounded-lg">
      <h2 className="text-label">Add a status</h2>
      <StatusForm workspace={workspace} types={types} />
    </section>
  )
}

export function StatusList({ workspace, statuses, types, manage }: { workspace: Workspace; statuses: Status[]; types: StatusType[]; manage: boolean }) {
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
    await reloadPage(router)
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
            types={types}
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

type RowProps = { workspace: Workspace; status: Status; statuses: Status[]; types: StatusType[]; manage: boolean; onUp?: () => void; onDown?: () => void }

function StatusRow({ workspace, status, statuses, types, manage, onUp, onDown }: RowProps) {
  const [editing, setEditing] = useState(false)
  const [deleting, setDeleting] = useState(false)

  if (editing) {
    return (
      <li className="px-6 py-3">
        <StatusForm workspace={workspace} status={status} types={types} onDone={() => setEditing(false)} />
      </li>
    )
  }

  return (
    <li className="flex flex-wrap items-center gap-3 px-6 py-2" style={{ minHeight: 56 }}>
      <StatusIcon status={status} />
      <div className="grid flex-grow" style={{ minWidth: 0 }}>
        <span className="text-label text-primary ellipsis">{status.name}</span>
        <span className="text-caption text-secondary">{status.type?.name ?? categoryLabel(status.category)}</span>
      </div>
      {manage ? (
        <>
          <IconButton aria-label={`Move ${status.name} up`} disabled={!onUp} onClick={onUp}>
            <ChevronUpIcon size={20} />
          </IconButton>
          <IconButton aria-label={`Move ${status.name} down`} disabled={!onDown} onClick={onDown}>
            <ChevronDownIcon size={20} />
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
    await reloadPage(router)
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


export function TypeList({ workspace, types, manage }: { workspace: Workspace; types: StatusType[]; manage: boolean }) {
  const [adding, setAdding] = useState(false)
  return (
    <section className="grid gap-4">
      <div className="flex items-center justify-between gap-3">
        <h2 className="text-label">Types</h2>
        {manage ? (
          <Button variant="secondary" onClick={() => setAdding(true)}>
            Add type
          </Button>
        ) : null}
      </div>
      <ul className="grid m-0 p-0 bg-surface border rounded-lg" style={{ listStyle: 'none' }} aria-label="Types">
        {CATEGORIES.map((c) => (
          <li key={c.value} className="flex items-center gap-3 px-6 py-2" style={{ minHeight: 56 }}>
            <StatusIcon status={{ category: c.value }} />
            <span className="text-label text-primary ellipsis flex-grow" style={{ minWidth: 0 }}>{c.label}</span>
            <span className="text-caption text-secondary">Built in</span>
          </li>
        ))}
        {types.map((t) => (
          <TypeRow key={t.id} workspace={workspace} type={t} manage={manage} />
        ))}
      </ul>
      {manage ? (
        <Dialog open={adding} onClose={() => setAdding(false)} title="Add type">
          <TypeForm key={String(adding)} workspace={workspace} onDone={() => setAdding(false)} />
        </Dialog>
      ) : null}
    </section>
  )
}

function TypeRow({ workspace, type, manage }: { workspace: Workspace; type: StatusType; manage: boolean }) {
  const router = useRouter()
  const [editing, setEditing] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const used = type.status_count === 1 ? '1 status' : `${type.status_count} statuses`

  const [error, remove, pending] = useActionState(async () => {
    try {
      await deleteStatusType(workspace.slug, type.id)
    } catch (e) {
      return toFormErrors(e).error
    }
    await reloadPage(router)
    setDeleting(false)
    return null
  }, null)

  return (
    <li className="flex flex-wrap items-center gap-3 px-6 py-2" style={{ minHeight: 56 }}>
      <StatusIcon status={type} />
      <div className="grid flex-grow" style={{ minWidth: 0 }}>
        <span className="text-label text-primary ellipsis">{type.name}</span>
        <span className="text-caption text-secondary">
          Counts as {categoryLabel(type.category)} · {used}
        </span>
      </div>
      {manage ? (
        <>
          <Button variant="ghost" onClick={() => setEditing(true)}>
            Edit
          </Button>
          <Button variant="ghost" onClick={() => setDeleting(true)}>
            Delete
          </Button>
          <Dialog open={editing} onClose={() => setEditing(false)} title={`Edit ${type.name}`}>
            <TypeForm key={String(editing)} workspace={workspace} type={type} onDone={() => setEditing(false)} />
          </Dialog>
          <Dialog
            open={deleting}
            onClose={() => setDeleting(false)}
            title={`Delete ${type.name}?`}
            description={`Statuses of this type become ${categoryLabel(type.category)} statuses.`}
          >
            <form action={remove} className="grid gap-4">
              <FormError>{error}</FormError>
              <Button type="submit" variant="danger" className="w-full" disabled={pending}>
                {pending ? 'Deleting…' : 'Delete type'}
              </Button>
            </form>
          </Dialog>
        </>
      ) : null}
    </li>
  )
}

function TypeForm({ workspace, type, onDone }: { workspace: Workspace; type?: StatusType; onDone: () => void }) {
  const router = useRouter()
  const [name, setName] = useState(type?.name ?? '')
  const [icon, setIcon] = useState(type?.icon ?? 'circle')
  const [color, setColor] = useState(type?.color ?? null)
  const [category, setCategory] = useState<StatusCategory>(type?.category ?? 'started')
  const shown = color ?? CATEGORY_HEX[category]

  const [{ errors, error }, submit, pending] = useActionState(async (): Promise<State> => {
    const errors = { name: statusTypeName(name), icon: icon ? null : 'Choose an icon.', color: color ? labelColor(color) : null }
    if (Object.values(errors).some(Boolean)) return { errors }
    const form = new FormData()
    form.set('name', name.trim())
    form.set('category', category)
    form.set('icon', icon)
    if (color) form.set('color', color.toLowerCase())
    try {
      await saveStatusType(workspace.slug, form, type?.id)
    } catch (e) {
      return toFormErrors(e)
    }
    await reloadPage(router)
    if (!type) {
      setName('')
      setIcon('circle')
      setColor(null)
    }
    onDone()
    return {}
  }, {})

  return (
    <form action={submit} noValidate className="grid gap-4">
      <Input label="Name" placeholder="In review" autoComplete="off" value={name} onChange={(e) => setName(e.target.value)} error={errors?.name} />
      <fieldset className="grid gap-2 m-0 p-0" style={{ border: 0 }}>
        <legend className="text-label text-secondary p-0" style={{ marginBottom: 8 }}>Icon</legend>
        <div className="flex flex-wrap gap-1">
          {STATUS_ICONS.map((key) => (
            <button key={key} type="button" className="icon-choice" aria-label={iconName(key)} aria-pressed={key === icon} onClick={() => setIcon(key)}>
              <StatusTypeIcon icon={key} size={18} color={key === icon ? shown : null} />
            </button>
          ))}
        </div>
        {errors?.icon ? <span className="text-caption text-accent">{errors.icon}</span> : null}
      </fieldset>
      <ColorPicker label="Color" value={shown} onChange={setColor} error={errors?.color} />
      <fieldset className="grid gap-1 m-0 p-0" style={{ border: 0 }}>
        <legend className="text-label text-secondary p-0" style={{ marginBottom: 8 }}>Counts as</legend>
        {CATEGORIES.map((c) => (
          <label key={c.value} className="flex items-center gap-2 text-body-sm text-primary" style={{ minHeight: 44, cursor: 'pointer' }}>
            <input type="radio" name="category" value={c.value} checked={category === c.value} onChange={() => setCategory(c.value)} style={{ accentColor: 'var(--link)' }} />
            <StatusIcon status={{ category: c.value }} />
            {c.label}
          </label>
        ))}
        {errors?.category ? <span className="text-caption text-accent">{errors.category}</span> : null}
      </fieldset>
      <FormError>{error}</FormError>
      <Button type="submit" className="w-full" disabled={pending}>
        {type ? (pending ? 'Saving…' : 'Save') : pending ? 'Adding…' : 'Add type'}
      </Button>
    </form>
  )
}
