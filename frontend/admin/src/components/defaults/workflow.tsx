import { useRouter } from '@tanstack/react-router'
import { startTransition, useActionState, useOptimistic, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { ColorPicker } from '@taskwell/shared/color-picker'
import { Dialog } from '@taskwell/shared/dialog'
import { IconButton } from '@taskwell/shared/icon-button'
import { ChevronDownIcon, ChevronUpIcon, STATUS_ICONS, StatusIcon, StatusTypeIcon } from '@taskwell/shared/icons'
import { Input } from '@taskwell/shared/input'
import { SmartAutocomplete, type Option } from '@taskwell/shared/smart-autocomplete'
import { labelColor, statusCategory, statusName, statusTypeName } from '@taskwell/shared/validate'
import { orderDefaultStatuses, saveDefault, toFormErrors, type DefaultStatus, type DefaultStatusType, type StatusCategory } from '@/api'
import { CATEGORIES, ConfirmDelete, FormError, Section, categoryLabel, type State } from './parts'

const CATEGORY_HEX: Record<StatusCategory, string> = { backlog: '#8a8f98', unstarted: '#8a8f98', started: '#f7f8f8', completed: '#00ba7c', canceled: '#8a8f98' }

const iconName = (key: string) => key.charAt(0).toUpperCase() + key.slice(1).replace('-', ' ')

const categoryOptions: Option[] = CATEGORIES.map((c) => ({ value: c.value, label: c.label, icon: <StatusIcon status={{ category: c.value }} /> }))

// A status's type as one value: `category:<c>` for a built-in one, `type:<id>` for a custom one.
const typeKey = (s: DefaultStatus) => (s.type_id ? `type:${s.type_id}` : `category:${s.category}`)

const typeOptions = (types: DefaultStatusType[]): Option[] => [
  ...categoryOptions.map((o) => ({ ...o, value: `category:${o.value}` })),
  ...types.map((t) => ({ value: `type:${t.id}`, label: t.name, icon: <StatusIcon status={t} /> })),
]

const lookOf = (s: DefaultStatus, types: DefaultStatusType[]) => types.find((t) => t.id === s.type_id) ?? s

export function Workflow({ types, statuses }: { types: DefaultStatusType[]; statuses: DefaultStatus[] }) {
  return (
    <Section title="Workflow" description="New workspaces start with these status types and statuses, in this order.">
      <StatusTypes types={types} />
      <Statuses statuses={statuses} types={types} />
    </Section>
  )
}

function StatusTypes({ types }: { types: DefaultStatusType[] }) {
  const [adding, setAdding] = useState(false)
  return (
    <div className="grid gap-3">
      <div className="flex items-center gap-3">
        <h3 className="text-label text-primary m-0 flex-grow">Status types</h3>
        <Button variant="secondary" onClick={() => setAdding(true)}>Add type</Button>
      </div>
      {types.length ? (
        <ul className="m-0 p-0 bg-surface border rounded-lg" style={{ listStyle: 'none' }} aria-label="Status types">
          {types.map((t) => (
            <TypeRow key={t.id} type={t} />
          ))}
        </ul>
      ) : (
        <p className="text-body-sm text-secondary m-0">No status types. Statuses use the built-in categories.</p>
      )}
      <Dialog open={adding} onClose={() => setAdding(false)} title="Add type" description="A type groups statuses under its own icon and color.">
        <TypeForm key={String(adding)} onDone={() => setAdding(false)} />
      </Dialog>
    </div>
  )
}

function TypeRow({ type }: { type: DefaultStatusType }) {
  const [editing, setEditing] = useState(false)
  return (
    <li className="defaults-row">
      <StatusIcon status={type} />
      <span className="text-label text-primary ellipsis flex-grow" style={{ minWidth: 0 }}>{type.name}</span>
      <Button variant="ghost" onClick={() => setEditing(true)}>Edit</Button>
      <ConfirmDelete kind="status-types" id={type.id} name={type.name} noun="type" description={`Its statuses keep the ${categoryLabel(type.category).toLowerCase()} category and lose this icon.`} />
      <Dialog open={editing} onClose={() => setEditing(false)} title={`Edit ${type.name}`}>
        <TypeForm key={String(editing)} type={type} onDone={() => setEditing(false)} />
      </Dialog>
    </li>
  )
}

function TypeForm({ type, onDone }: { type?: DefaultStatusType; onDone: () => void }) {
  const router = useRouter()
  const [name, setName] = useState(type?.name ?? '')
  // A type is its own identity, not "works like" a built-in; new ones behave as open, in-flight work.
  const category: StatusCategory = type?.category ?? 'started'
  const [icon, setIcon] = useState(type?.icon ?? 'circle')
  const [color, setColor] = useState(type?.color ?? null)
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
      await saveDefault<DefaultStatusType>('status-types', form, type?.id)
    } catch (e) {
      return toFormErrors(e)
    }
    await router.invalidate()
    onDone()
    return {}
  }, {})

  return (
    <form action={submit} noValidate className="grid gap-4">
      <Input label="Name" placeholder="In review" autoComplete="off" value={name} onChange={(e) => setName(e.target.value)} error={errors?.name} />
      <fieldset className="grid gap-2 m-0 p-0" style={{ border: 0 }}>
        <legend className="text-label text-secondary p-0 mb-2">Icon</legend>
        <div className="flex flex-wrap gap-1">
          {STATUS_ICONS.map((key) => (
            <button key={key} type="button" className="icon-choice" aria-label={iconName(key)} aria-pressed={key === icon} onClick={() => setIcon(key)}>
              <StatusTypeIcon icon={key} size={18} color={key === icon ? shown : null} />
            </button>
          ))}
        </div>
        <FormError>{errors?.icon}</FormError>
      </fieldset>
      <ColorPicker label="Color" value={shown} onChange={setColor} error={errors?.color} />
      <FormError>{error}</FormError>
      <Button type="submit" className="w-full" disabled={pending}>
        {type ? (pending ? 'Saving…' : 'Save') : pending ? 'Adding…' : 'Add type'}
      </Button>
    </form>
  )
}

function Statuses({ statuses, types }: { statuses: DefaultStatus[]; types: DefaultStatusType[] }) {
  const router = useRouter()
  const [order, setOrder] = useOptimistic(statuses)
  const [adding, setAdding] = useState(false)

  const [moveError, reorder] = useActionState(async (_prev: string | null, next: DefaultStatus[]) => {
    setOrder(next)
    try {
      await orderDefaultStatuses(next.map((s) => s.id))
    } catch (e) {
      const { errors, error } = toFormErrors(e)
      return errors.id ?? error
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
    <div className="grid gap-3">
      <div className="flex items-center gap-3">
        <h3 className="text-label text-primary m-0 flex-grow">Statuses</h3>
        <Button variant="secondary" onClick={() => setAdding(true)}>Add status</Button>
      </div>
      <FormError>{moveError}</FormError>
      <ul className="m-0 p-0 bg-surface border rounded-lg" style={{ listStyle: 'none' }} aria-label="Statuses">
        {order.map((s, i) => (
          <StatusRow
            key={s.id}
            status={s}
            types={types}
            last={order.length < 2}
            onUp={i > 0 ? () => move(i, -1) : undefined}
            onDown={i < order.length - 1 ? () => move(i, 1) : undefined}
          />
        ))}
        {adding ? (
          <li className="defaults-row">
            <StatusForm types={types} onDone={() => setAdding(false)} />
          </li>
        ) : null}
      </ul>
    </div>
  )
}

type RowProps = { status: DefaultStatus; types: DefaultStatusType[]; last: boolean; onUp?: () => void; onDown?: () => void }

function StatusRow({ status, types, last, onUp, onDown }: RowProps) {
  const [editing, setEditing] = useState(false)
  if (editing) {
    return (
      <li className="defaults-row">
        <StatusForm status={status} types={types} onDone={() => setEditing(false)} />
      </li>
    )
  }
  const type = types.find((t) => t.id === status.type_id)
  return (
    <li className="defaults-row">
      <StatusIcon status={lookOf(status, types)} />
      <span className="text-body-sm text-primary ellipsis flex-grow" style={{ minWidth: 0 }}>{status.name}</span>
      <span className="text-caption text-secondary">{type?.name ?? categoryLabel(status.category)}</span>
      <IconButton aria-label={`Move ${status.name} up`} tooltip="Move up" disabled={!onUp} onClick={onUp}>
        <ChevronUpIcon size={18} />
      </IconButton>
      <IconButton aria-label={`Move ${status.name} down`} tooltip="Move down" disabled={!onDown} onClick={onDown}>
        <ChevronDownIcon size={18} />
      </IconButton>
      <Button variant="ghost" onClick={() => setEditing(true)}>Edit</Button>
      <ConfirmDelete kind="statuses" id={status.id} name={status.name} noun="status" disabled={last} description="New workspaces won't get this status. Templates using it fall back to the default status." />
    </li>
  )
}

function StatusForm({ status, types, onDone }: { status?: DefaultStatus; types: DefaultStatusType[]; onDone: () => void }) {
  const router = useRouter()
  const [name, setName] = useState(status?.name ?? '')
  const [choice, setChoice] = useState(status ? typeKey(status) : 'category:unstarted')
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
      await saveDefault<DefaultStatus>('statuses', form, status?.id)
    } catch (e) {
      return toFormErrors(e)
    }
    await router.invalidate()
    onDone()
    return {}
  }, {})

  return (
    <form action={submit} noValidate className="grid gap-2 flex-grow py-2" onKeyDown={(e) => e.key === 'Escape' && onDone()}>
      <div className="flex flex-wrap items-start gap-3">
        <div className="flex-grow" style={{ minWidth: 160 }}>
          <Input aria-label="Status name" placeholder="Status name" autoComplete="off" autoFocus value={name} onChange={(e) => setName(e.target.value)} error={errors?.name} />
        </div>
        <div className="flex-noshrink" style={{ width: 200 }}>
          <SmartAutocomplete
            aria-label="Category or type"
            placeholder="Pick a type"
            options={options}
            value={options.find((o) => o.value === choice) ?? null}
            onChange={(o) => o && setChoice(o.value)}
            error={errors?.type_id ?? errors?.category}
          />
        </div>
        <Button type="submit" disabled={pending}>
          {status ? (pending ? 'Saving…' : 'Save') : pending ? 'Adding…' : 'Add'}
        </Button>
        <Button variant="ghost" onClick={onDone} disabled={pending}>
          Cancel
        </Button>
      </div>
      <FormError>{error}</FormError>
    </form>
  )
}
