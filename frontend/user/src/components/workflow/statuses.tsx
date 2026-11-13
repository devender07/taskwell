import { useRouter } from '@tanstack/react-router'
import { startTransition, useActionState, useOptimistic, useState, type DragEvent, type ReactNode } from 'react'
import { Button } from '@taskwell/shared/button'
import { Dialog } from '@taskwell/shared/dialog'
import { IconButton } from '@taskwell/shared/icon-button'
import { Input } from '@taskwell/shared/input'
import { Popover } from '@taskwell/shared/popover'
import { SmartAutocomplete } from '@taskwell/shared/smart-autocomplete'
import { ColorPicker } from '@taskwell/shared/color-picker'
import { STATUS_ICONS, StatusTypeIcon, ChevronDownIcon, ChevronUpIcon, GripIcon, MoreIcon, PlusIcon } from '@taskwell/shared/icons'
import type { Option } from '@taskwell/shared/smart-autocomplete'
import { labelColor, statusCategory, statusName, statusTypeName } from '@taskwell/shared/validate'
import { api, deleteStatusType, saveStatusType, toFormErrors, type FieldErrors, type Status, type StatusCategory, type StatusType, type Workspace, workspacePath } from '@/api'
import { reloadPage } from '@/realtime'
import { FormError } from '@/components/auth/auth-card'
import { CATEGORIES, StatusIcon, categoryLabel, statusOptions } from '@/components/issues/fields'
import './workflow.css'
import { byId, extractDataFromArray } from '@taskwell/shared/list'

type State = { errors?: FieldErrors; error?: string | null }

const statusesPath = (workspace: Workspace) => `${workspacePath(workspace.slug)}/statuses`

const CATEGORY_HEX: Record<StatusCategory, string> = { backlog: '#8a8f98', unstarted: '#8a8f98', started: '#f7f8f8', completed: '#00ba7c', canceled: '#8a8f98' }

const iconName = (key: string) => key.charAt(0).toUpperCase() + key.slice(1).replace('-', ' ')

// A status's type as one value: `category:<c>` for a built-in type, `type:<id>` for a custom one.
const typeKey = (s: Status) => (s.type ? `type:${s.type.id}` : `category:${s.category}`)

function typeOptions(types: StatusType[]): Option[] {
  return [
    ...CATEGORIES.map((c) => ({ value: `category:${c.value}`, label: c.label, icon: <StatusIcon status={{ category: c.value }} /> })),
    ...types.map((t) => ({ value: `type:${t.id}`, label: t.name, icon: <StatusIcon status={t} /> })),
  ]
}

type Group = { key: string; name: string; type?: StatusType; look: { category: StatusCategory; icon?: string | null; color?: string | null }; statuses: Status[] }

// One group per type, ordered by where its first status sits; empty groups keep the default order at the end.
function groupStatuses(statuses: Status[], types: StatusType[]): Group[] {
  const [backlog, unstarted, started, completed, canceled] = CATEGORIES.map((c): Group => {
    const key = `category:${c.value}`
    return { key, name: c.label, look: { category: c.value }, statuses: statuses.filter((s) => typeKey(s) === key) }
  })
  const custom = types.map((t): Group => ({ key: `type:${t.id}`, name: t.name, type: t, look: t, statuses: statuses.filter((s) => s.type?.id === t.id) }))
  const first = (g: Group) => (g.statuses.length ? statuses.indexOf(g.statuses[0]!) : Infinity)
  return [backlog!, unstarted!, started!, ...custom, completed!, canceled!].sort((a, b) => first(a) - first(b))
}

export function AddType({ workspace }: { workspace: Workspace }) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <Button variant="secondary" onClick={() => setOpen(true)}>
        <PlusIcon size={16} /> Add type
      </Button>
      <Dialog open={open} onClose={() => setOpen(false)} title="Add type" description="A type groups statuses under its own icon and color.">
        <TypeForm key={String(open)} workspace={workspace} onDone={() => setOpen(false)} />
      </Dialog>
    </>
  )
}

export function Workflow({ workspace, statuses, types, manage }: { workspace: Workspace; statuses: Status[]; types: StatusType[]; manage: boolean }) {
  const router = useRouter()
  const [order, setOrder] = useOptimistic(statuses)
  const [drag, setDrag] = useState<{ key: string; over: string | null } | null>(null)
  const groups = groupStatuses(order, types)

  // Sends every status in the order shown, so positions follow the grouping.
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

  function move(group: Group, i: number, dir: -1 | 1) {
    const moved = [...group.statuses]
    ;[moved[i], moved[i + dir]] = [moved[i + dir]!, moved[i]!]
    startTransition(() => reorder(groups.flatMap((g) => (g === group ? moved : g.statuses))))
  }

  // ponytail: a group's place comes from its statuses' positions, so an empty group can't be moved; add a type position column if that matters.
  function moveGroup(from: number, to: number) {
    if (from === to || from < 0 || to < 0 || to >= groups.length) return
    const next = [...groups]
    next.splice(to, 0, ...next.splice(from, 1))
    startTransition(() => reorder(next.flatMap((g) => g.statuses)))
  }

  const dragProps = (g: Group, i: number) =>
    manage
      ? {
          onDragOver: (e: DragEvent) => {
            if (!drag) return
            e.preventDefault()
            if (drag.over !== g.key) setDrag({ ...drag, over: g.key })
          },
          onDrop: (e: DragEvent) => {
            e.preventDefault()
            if (drag) moveGroup(groups.findIndex((x) => x.key === drag.key), i)
            setDrag(null)
          },
          'data-over': drag && drag.over === g.key && drag.key !== g.key ? '' : undefined,
          'data-dragging': drag?.key === g.key ? '' : undefined,
        }
      : {}

  const handle = (g: Group, i: number) =>
    manage && g.statuses.length ? (
      <button
        type="button"
        className="workflow-handle"
        draggable
        aria-label={`Reorder ${g.name}. Use the up and down arrow keys.`}
        onDragStart={(e) => {
          e.dataTransfer.effectAllowed = 'move'
          const card = e.currentTarget.closest('section')
          if (card) e.dataTransfer.setDragImage(card, 16, 16)
          setDrag({ key: g.key, over: null })
        }}
        onDragEnd={() => setDrag(null)}
        onKeyDown={(e) => {
          if (e.key !== 'ArrowUp' && e.key !== 'ArrowDown') return
          e.preventDefault()
          moveGroup(i, i + (e.key === 'ArrowUp' ? -1 : 1))
        }}
      >
        <GripIcon size={16} aria-hidden />
      </button>
    ) : null

  return (
    <div className="grid gap-4">
      <FormError>{moveError}</FormError>
      {groups.map((g, i) => (
        <GroupCard
          key={g.key}
          workspace={workspace}
          group={g}
          statuses={order}
          types={types}
          manage={manage}
          handle={handle(g, i)}
          dragProps={dragProps(g, i)}
          onMove={(i, dir) => move(g, i, dir)}
        />
      ))}
    </div>
  )
}

type GroupProps = {
  workspace: Workspace
  group: Group
  statuses: Status[]
  types: StatusType[]
  manage: boolean
  handle: ReactNode
  dragProps: object
  onMove: (i: number, dir: -1 | 1) => void
}

function GroupCard({ workspace, group, statuses, types, manage, handle, dragProps, onMove }: GroupProps) {
  const [adding, setAdding] = useState(false)
  const count = group.statuses.length
  return (
    <section className="workflow-group bg-surface border rounded-lg" aria-label={group.name} {...dragProps}>
      <header className="workflow-group-header">
        {handle}
        <StatusIcon status={group.look} />
        <h2 className="text-label text-primary m-0 ellipsis">{group.name}</h2>
        <span className="text-caption text-secondary">{count}</span>
        {group.type ? <span className="workflow-tag">Custom</span> : null}
        <span className="flex-grow" />
        {manage && group.type ? <TypeMenu workspace={workspace} type={group.type} /> : null}
        {manage ? (
          <IconButton aria-label={`Add status to ${group.name}`} tooltip="Add status" onClick={() => setAdding(true)}>
            <PlusIcon size={18} />
          </IconButton>
        ) : null}
      </header>
      <ul className="m-0 p-0" style={{ listStyle: 'none' }} aria-label={`${group.name} statuses`}>
        {group.statuses.map((s, i) => (
          <StatusRow
            key={s.id}
            workspace={workspace}
            status={s}
            statuses={statuses}
            types={types}
            manage={manage}
            onUp={i > 0 ? () => onMove(i, -1) : undefined}
            onDown={i < count - 1 ? () => onMove(i, 1) : undefined}
          />
        ))}
        {adding ? (
          <li className="workflow-row">
            <StatusForm workspace={workspace} choice={group.key} types={types} onDone={() => setAdding(false)} />
          </li>
        ) : count === 0 ? (
          <li className="workflow-row text-caption text-secondary">No statuses yet.</li>
        ) : null}
      </ul>
    </section>
  )
}

function TypeMenu({ workspace, type }: { workspace: Workspace; type: StatusType }) {
  const router = useRouter()
  const [dialog, setDialog] = useState<'edit' | 'delete' | null>(null)
  const close = () => setDialog(null)

  const [error, remove, pending] = useActionState(async () => {
    try {
      await deleteStatusType(workspace.slug, type.id)
    } catch (e) {
      return toFormErrors(e).error
    }
    await reloadPage(router)
    close()
    return null
  }, null)

  return (
    <>
      <Menu label={`${type.name} type actions`} items={[['Edit type', () => setDialog('edit')], ['Delete type', () => setDialog('delete')]]} />
      <Dialog open={dialog === 'edit'} onClose={close} title={`Edit ${type.name}`}>
        <TypeForm key={String(dialog)} workspace={workspace} type={type} onDone={close} />
      </Dialog>
      <Dialog
        open={dialog === 'delete'}
        onClose={close}
        title={`Delete ${type.name}?`}
        description={type.status_count ? `Its statuses move to ${categoryLabel(type.category)} and lose this icon.` : 'No statuses use it.'}
      >
        <form action={remove} className="grid gap-4">
          <FormError>{error}</FormError>
          <Button type="submit" variant="danger" className="w-full" disabled={pending}>
            {pending ? 'Deleting…' : 'Delete type'}
          </Button>
        </form>
      </Dialog>
    </>
  )
}

function Menu({ label, items }: { label: string; items: [string, () => void, boolean?][] }) {
  return (
    <Popover
      aria-label={label}
      placement="bottom-end"
      trigger={(props) => (
        <IconButton aria-label={label} tooltip="More actions" {...props}>
          <MoreIcon size={18} />
        </IconButton>
      )}
    >
      {(closeMenu) => (
        <div style={{ width: 180 }}>
          {items.map(([text, run, disabled]) => (
            <button
              key={text}
              type="button"
              className="popover-item"
              disabled={disabled}
              onClick={() => {
                closeMenu()
                run()
              }}
            >
              {text}
            </button>
          ))}
        </div>
      )}
    </Popover>
  )
}

type RowProps = { workspace: Workspace; status: Status; statuses: Status[]; types: StatusType[]; manage: boolean; onUp?: () => void; onDown?: () => void }

function StatusRow({ workspace, status, statuses, types, manage, onUp, onDown }: RowProps) {
  const [mode, setMode] = useState<'edit' | 'delete' | null>(null)

  if (mode === 'edit') {
    return (
      <li className="workflow-row">
        <StatusForm workspace={workspace} status={status} choice={typeKey(status)} types={types} onDone={() => setMode(null)} />
      </li>
    )
  }

  return (
    <li className="workflow-row">
      <StatusIcon status={status} />
      <span className="text-body-sm text-primary ellipsis flex-grow" style={{ minWidth: 0 }}>{status.name}</span>
      {manage ? (
        <div className="workflow-actions">
          <IconButton aria-label={`Move ${status.name} up`} tooltip="Move up" disabled={!onUp} onClick={onUp}>
            <ChevronUpIcon size={18} />
          </IconButton>
          <IconButton aria-label={`Move ${status.name} down`} tooltip="Move down" disabled={!onDown} onClick={onDown}>
            <ChevronDownIcon size={18} />
          </IconButton>
          <Menu label={`${status.name} actions`} items={[['Edit', () => setMode('edit')], ['Delete', () => setMode('delete'), statuses.length < 2]]} />
          <Dialog open={mode === 'delete'} onClose={() => setMode(null)} title={`Delete ${status.name}?`} description="Issues with this status move to the one you pick.">
            <DeleteStatusForm workspace={workspace} status={status} statuses={statuses} onDone={() => setMode(null)} />
          </Dialog>
        </div>
      ) : null}
    </li>
  )
}

// Adds a status to a type (choice), or edits one: then its type can change too.
function StatusForm({ workspace, status, choice: initial, types, onDone }: { workspace: Workspace; status?: Status; choice: string; types: StatusType[]; onDone: () => void }) {
  const router = useRouter()
  const [name, setName] = useState(extractDataFromArray(status, 'name', ''))
  const [choice, setChoice] = useState(initial)
  const options = typeOptions(types)

  const [{ errors, error }, submit, pending] = useActionState(async (): Promise<State> => {
    const [kind, ref = ''] = choice.split(':')
    const type = kind === 'type' ? byId(types, ref) : undefined
    const category = extractDataFromArray(type, 'category', ref)
    const errors = { name: statusName(name), type_id: kind === 'type' && !type ? 'Choose a type.' : statusCategory(category) }
    if (Object.values(errors).some(Boolean)) return { errors }
    const form = new FormData()
    form.set('name', name.trim())
    form.set('category', category)
    form.set('type_id', extractDataFromArray(type, 'id', ''))
    try {
      await api<Status>(status ? `${statusesPath(workspace)}/${encodeURIComponent(status.id)}` : statusesPath(workspace), form)
    } catch (e) {
      return toFormErrors(e)
    }
    await reloadPage(router)
    onDone()
    return {}
  }, {})

  return (
    <form action={submit} noValidate className="grid gap-2 flex-grow" onKeyDown={(e) => e.key === 'Escape' && onDone()}>
      <div className="flex flex-wrap items-start gap-3">
        <div className="flex-grow" style={{ minWidth: 160 }}>
          <Input aria-label="Status name" placeholder="Status name" autoComplete="off" autoFocus value={name} onChange={(e) => setName(e.target.value)} error={errors?.name} />
        </div>
        {status ? (
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
        ) : null}
        <Button type="submit" disabled={pending}>
          {status ? (pending ? 'Saving…' : 'Save') : pending ? 'Adding…' : 'Add'}
        </Button>
        <Button variant="ghost" onClick={onDone} disabled={pending}>
          Cancel
        </Button>
      </div>
      {status ? null : <FieldError>{errors?.type_id ?? errors?.category}</FieldError>}
      <FormError>{error}</FormError>
    </form>
  )
}

const FieldError = ({ children }: { children?: string | null }) => (children ? <span className="text-caption text-accent">{children}</span> : null)

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


function TypeForm({ workspace, type, onDone }: { workspace: Workspace; type?: StatusType; onDone: () => void }) {
  const router = useRouter()
  const [name, setName] = useState(extractDataFromArray(type, 'name', ''))
  const [icon, setIcon] = useState(extractDataFromArray(type, 'icon', 'circle'))
  const [color, setColor] = useState(extractDataFromArray(type, 'color', null))
  // A type is its own identity, not "works like" a built-in; new ones behave as open, in-flight work.
  const category: StatusCategory = extractDataFromArray(type, 'category', 'started')
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
      <div className="workflow-preview" aria-hidden>
        <StatusTypeIcon icon={icon} color={shown} size={16} />
        <span className="text-label text-primary ellipsis">{name.trim() || 'New type'}</span>
      </div>
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
      <FormError>{error}</FormError>
      <Button type="submit" className="w-full" disabled={pending}>
        {type ? (pending ? 'Saving…' : 'Save') : pending ? 'Adding…' : 'Add type'}
      </Button>
    </form>
  )
}
