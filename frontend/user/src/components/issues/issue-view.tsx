import { Link, useNavigate, useRouter } from '@tanstack/react-router'
import { startTransition, useActionState, useEffect, useOptimistic, useRef, useState, type PointerEvent as ReactPointerEvent, type ReactNode } from 'react'
import { Avatar } from '@tether/shared/avatar'
import { Button } from '@tether/shared/button'
import { Checkbox } from '@tether/shared/checkbox'
import { Popover } from '@tether/shared/popover'
import { SmartAutocomplete, type Option } from '@tether/shared/smart-autocomplete'
import { Tabs } from '@tether/shared/tabs'
import { api, toFormErrors, type Issue, type Label, type Member, type Status, type Workspace } from '@/api'
import { FormError } from '@/components/auth/auth-card'
import { LabelDot, StatusIcon, formatDate, overdue, priorityLabel } from './fields'
import { issuePath } from './issue-form'
import {
  FILTER_KEYS,
  PRIORITY_ORDER,
  filterIssues,
  filterParam,
  filterValues,
  groupIssues,
  sortIssues,
  type FilterKey,
  type GroupBy,
  type IssueGroup,
  type SortBy,
  type ViewSearch,
} from './view-search'
import './issues.css'

type Props = {
  workspace: Workspace
  issues: Issue[]
  statuses: Status[]
  labels: Label[]
  members: Member[]
  userId: string
  search: ViewSearch
  mine?: boolean
}
type Choice = { key: FilterKey; title: string; options: { value: string; label: string; icon?: ReactNode }[] }
type Move = { issue: Issue; form: FormData; next: Partial<Issue> }

const views = [
  { value: 'list', label: 'List' },
  { value: 'board', label: 'Board' },
] as const
const sortOptions: Option[] = [
  { value: 'priority', label: 'Priority' },
  { value: 'updated', label: 'Last updated' },
  { value: 'created', label: 'Created' },
  { value: 'due', label: 'Due date' },
]

// Toolbar plus list or board for an issue array; display options and filters live in the URL.
export function IssueView({ workspace, issues: saved, statuses, labels, members, userId, search, mine }: Props) {
  const navigate = useNavigate()
  const router = useRouter()
  const [issues, apply] = useOptimistic(saved, (cur, { id, next }: { id: string; next: Partial<Issue> }) =>
    cur.map((i) => (i.id === id ? { ...i, ...next } : i)),
  )
  const [error, move] = useActionState(async (_prev: string | null, { issue, form, next }: Move) => {
    apply({ id: issue.id, next })
    try {
      await api<Issue>(issuePath(workspace, issue.identifier), form)
    } catch (e) {
      const { errors, error } = toFormErrors(e)
      return Object.values(errors).find(Boolean) ?? error
    }
    await router.invalidate()
    return null
  }, null)

  const view = search.view ?? 'list'
  const wanted = search.group ?? 'status'
  const allowed = mine && wanted === 'assignee' ? 'status' : wanted
  const group: GroupBy = view === 'board' && allowed === 'none' ? 'status' : allowed
  const sort: SortBy = search.sort ?? 'priority'
  const setSearch = (next: ViewSearch) => navigate({ to: '.', search: next })

  const choices: Choice[] = [
    { key: 'status', title: 'Status', options: statuses.map((s) => ({ value: s.id, label: s.name, icon: <StatusIcon category={s.category} /> })) },
    ...(mine
      ? []
      : [
          {
            key: 'assignee' as const,
            title: 'Assignee',
            options: [
              { value: 'me', label: 'Me' },
              { value: 'none', label: 'No assignee' },
              ...members.filter((m) => m.id !== userId).map((m) => ({ value: m.id, label: m.name })),
            ],
          },
        ]),
    { key: 'priority', title: 'Priority', options: PRIORITY_ORDER.map((p) => ({ value: String(p), label: priorityLabel(p) })) },
    { key: 'label', title: 'Label', options: labels.map((l) => ({ value: l.id, label: l.name, icon: <LabelDot color={l.color} /> })) },
  ]
  const active = choices
    .map((c) => ({ ...c, values: filterValues(search, c.key).filter((v) => c.options.some((o) => o.value === v)) }))
    .filter((c) => c.values.length > 0)
  const effective: ViewSearch = Object.fromEntries(active.map((c) => [c.key, filterParam(c.values)]))

  const shown = sortIssues(filterIssues(issues, effective, userId), sort)
  const groups = groupIssues(shown, group, { statuses, members, userId })

  const setFilter = (key: FilterKey, values: string[]) => setSearch({ ...search, [key]: filterParam(values) })
  const clearFilters = () => setSearch(Object.fromEntries(Object.entries(search).filter(([k]) => !FILTER_KEYS.includes(k as FilterKey))))

  const groupOptions: Option[] = [
    { value: 'status', label: 'Status' },
    ...(mine ? [] : [{ value: 'assignee', label: 'Assignee' }]),
    { value: 'priority', label: 'Priority' },
    ...(view === 'list' ? [{ value: 'none', label: 'No grouping' }] : []),
  ]

  function onMove(issue: Issue, key: string) {
    const form = new FormData()
    let next: Partial<Issue>
    if (group === 'status') {
      const status = statuses.find((s) => s.id === key)
      if (!status) return
      form.set('status_id', key)
      next = { status }
    } else if (group === 'assignee') {
      const m = members.find((m) => m.id === key)
      form.set('assignee_id', key)
      next = { assignee: m ? { id: m.id, name: m.name } : null }
    } else {
      form.set('priority', key)
      next = { priority: Number(key) }
    }
    startTransition(() => move({ issue, form, next }))
  }

  return (
    <div className="grid gap-4">
      <div className="flex flex-wrap items-center gap-3">
        <Tabs
          aria-label="View"
          className="w-fit"
          items={[...views]}
          value={view}
          onChange={(v) => setSearch({ ...search, view: v === 'board' ? 'board' : undefined })}
        />
        <Select
          label="Group"
          options={groupOptions}
          value={group}
          onChange={(v) => setSearch({ ...search, group: v === 'status' ? undefined : (v as ViewSearch['group']) })}
        />
        <Select
          label="Sort"
          options={sortOptions}
          value={sort}
          onChange={(v) => setSearch({ ...search, sort: v === 'priority' ? undefined : (v as ViewSearch['sort']) })}
        />
        <FilterMenu choices={choices} search={search} onChange={setFilter} />
        <span className="text-caption text-secondary" style={{ marginLeft: 'auto' }}>
          {shown.length} {shown.length === 1 ? 'issue' : 'issues'}
        </span>
      </div>
      {active.length > 0 ? (
        <div className="flex flex-wrap items-center gap-2">
          {active.map((c) => (
            <button key={c.key} type="button" className="filter-chip" aria-label={`Remove ${c.title.toLowerCase()} filter`} onClick={() => setFilter(c.key, [])}>
              {c.title}: {c.values.map((v) => c.options.find((o) => o.value === v)?.label).join(', ')}
              <span aria-hidden>×</span>
            </button>
          ))}
          <Button variant="ghost" onClick={clearFilters}>
            Clear filters
          </Button>
        </div>
      ) : null}
      <FormError>{error}</FormError>
      {view === 'board' ? (
        <Board workspace={workspace.slug} groups={groups} by={group} onMove={onMove} />
      ) : shown.length === 0 ? (
        <p className="text-body-sm text-secondary p-6 bg-surface border rounded-lg text-center">No issues match these filters.</p>
      ) : (
        groups
          .filter((g) => g.issues.length > 0)
          .map((g) => (
            <section key={g.key} className="bg-surface border rounded-lg overflow-hidden" aria-label={group === 'none' ? 'Issues' : g.label}>
              {group === 'none' ? null : (
                <header className="issue-group-header">
                  <GroupIcon group={g} by={group} />
                  <span className="text-label">{g.label}</span>
                  <span className="text-caption text-secondary">{g.issues.length}</span>
                </header>
              )}
              <ul className="grid m-0 p-0" style={{ listStyle: 'none' }}>
                {g.issues.map((issue) => (
                  <li key={issue.id}>
                    <IssueRow workspace={workspace.slug} issue={issue} />
                  </li>
                ))}
              </ul>
            </section>
          ))
      )}
    </div>
  )
}

function Select({ label, options, value, onChange }: { label: string; options: Option[]; value: string; onChange: (v: string) => void }) {
  return (
    <div className="flex items-center gap-2">
      <span className="text-caption text-secondary">{label}</span>
      <div style={{ width: 160 }}>
        <SmartAutocomplete
          aria-label={label}
          options={options}
          value={options.find((o) => o.value === value) ?? null}
          onChange={(o) => o && o.value !== value && onChange(o.value)}
        />
      </div>
    </div>
  )
}

function FilterMenu({ choices, search, onChange }: { choices: Choice[]; search: ViewSearch; onChange: (key: FilterKey, values: string[]) => void }) {
  return (
    <Popover
      aria-label="Filters"
      trigger={(props) => (
        <Button variant="secondary" {...props}>
          Filter
        </Button>
      )}
    >
      <div className="grid gap-4 p-3" style={{ maxHeight: 420, overflowY: 'auto' }}>
        {choices
          .filter((c) => c.options.length > 0)
          .map((c) => {
            const values = filterValues(search, c.key)
            return (
              <fieldset key={c.key} className="grid gap-2 m-0 p-0" style={{ border: 0 }}>
                <legend className="text-label text-secondary mb-2">{c.title}</legend>
                {c.options.map((o) => (
                  <Checkbox
                    key={o.value}
                    checked={values.includes(o.value)}
                    onChange={(e) => onChange(c.key, e.target.checked ? [...values, o.value] : values.filter((v) => v !== o.value))}
                    label={
                      <span className="flex items-center gap-2" style={{ minWidth: 0 }}>
                        {o.icon}
                        <span className="ellipsis">{o.label}</span>
                      </span>
                    }
                  />
                ))}
              </fieldset>
            )
          })}
      </div>
    </Popover>
  )
}

function GroupIcon({ group, by }: { group: IssueGroup; by: GroupBy }) {
  if (group.status) return <StatusIcon category={group.status.category} />
  if (by === 'assignee' && group.key) return <Avatar name={group.label} size={20} />
  return null
}

export function IssueRow({ workspace, issue }: { workspace: string; issue: Issue }) {
  return (
    <Link to="/$workspace/issue/$identifier" params={{ workspace, identifier: issue.identifier }} className="issue-row">
      <StatusIcon category={issue.status.category} label={issue.status.name} />
      <span className="issue-id flex-noshrink" style={{ minWidth: 64 }}>{issue.identifier}</span>
      {issue.parent ? <span className="issue-id flex-noshrink">↳ {issue.parent.identifier}</span> : null}
      <span className="text-label text-primary ellipsis flex-grow">{issue.title}</span>
      <LabelDots issue={issue} />
      <span className="issue-row-wide text-caption text-secondary flex-noshrink">{issue.status.name}</span>
      {issue.priority ? <span className="text-caption text-secondary flex-noshrink">{priorityLabel(issue.priority)}</span> : null}
      <DueDate issue={issue} />
      <Assignee issue={issue} />
    </Link>
  )
}

function LabelDots({ issue }: { issue: Issue }) {
  if (!issue.labels.length) return null
  return (
    <span role="img" aria-label={`Labels: ${issue.labels.map((l) => l.name).join(', ')}`} className="flex items-center gap-1 flex-noshrink">
      {issue.labels.map((l) => (
        <LabelDot key={l.id} color={l.color} />
      ))}
    </span>
  )
}

function DueDate({ issue }: { issue: Issue }) {
  if (!issue.due_date) return null
  return (
    <span className={`text-caption flex-noshrink ${overdue(issue.due_date, issue.status.category) ? 'text-accent' : 'text-secondary'}`}>
      {formatDate(issue.due_date)}
    </span>
  )
}

function Assignee({ issue }: { issue: Issue }) {
  if (!issue.assignee) return null
  return (
    <span role="img" aria-label={`Assigned to ${issue.assignee.name}`} title={issue.assignee.name} className="inline-grid flex-noshrink">
      <Avatar name={issue.assignee.name} size={24} />
    </span>
  )
}

function CardBody({ issue }: { issue: Issue }) {
  return (
    <>
      <span className="flex items-center justify-between gap-2">
        <span className="issue-id ellipsis">{issue.identifier}</span>
        <Assignee issue={issue} />
      </span>
      <span className="text-label text-primary ellipsis-2">{issue.title}</span>
      {issue.labels.length || issue.priority || issue.due_date ? (
        <span className="flex flex-wrap items-center gap-2">
          <LabelDots issue={issue} />
          {issue.priority ? <span className="text-caption text-secondary">{priorityLabel(issue.priority)}</span> : null}
          <DueDate issue={issue} />
        </span>
      ) : null}
    </>
  )
}

type Drag = { issue: Issue; over: string | null; width: number }

const HOLD_MS = 150
const SLOP_PX = 6
const EDGE_PX = 56 // dragging this close to the board's side scrolls it, so off-screen columns are reachable

// Columns are drop targets ([data-column]); cards drag with pointer events so mouse, pen and touch all work.
// Touch needs a short hold first, so a swipe still scrolls the board.
// ponytail: no keyboard drag; change status, assignee or priority on the issue page instead.
function Board({ workspace, groups, by, onMove }: { workspace: string; groups: IssueGroup[]; by: GroupBy; onMove: (issue: Issue, key: string) => void }) {
  const [drag, setDrag] = useState<Drag | null>(null)
  const boardRef = useRef<HTMLDivElement>(null)
  const dragging = useRef(false)
  const suppressClick = useRef(false)
  const pos = useRef({ x: 0, y: 0 })
  const ghost = useRef<HTMLDivElement | null>(null)
  const place = () => {
    if (ghost.current) ghost.current.style.transform = `translate(${pos.current.x}px, ${pos.current.y}px)`
  }

  useEffect(() => {
    const board = boardRef.current
    if (!board) return
    const block = (e: TouchEvent) => dragging.current && e.preventDefault()
    board.addEventListener('touchmove', block, { passive: false })
    return () => board.removeEventListener('touchmove', block)
  }, [])

  function press(e: ReactPointerEvent<HTMLAnchorElement>, issue: Issue, from: string) {
    suppressClick.current = false
    if (!issue.can_edit || !e.isPrimary || e.button !== 0) return
    const { pointerId, pointerType } = e
    const start = { x: e.clientX, y: e.clientY }
    const rect = e.currentTarget.getBoundingClientRect()
    const offset = { x: start.x - rect.left, y: start.y - rect.top }
    const columnAt = (x: number, y: number) => document.elementFromPoint(x, y)?.closest<HTMLElement>('[data-column]')?.dataset.column ?? null
    pos.current = { x: rect.left, y: rect.top }

    const begin = () => {
      dragging.current = true
      suppressClick.current = true
      setDrag({ issue, over: from, width: rect.width })
    }
    const timer = setTimeout(begin, HOLD_MS)
    const last = { x: start.x, y: start.y }
    let frame = 0
    const autoScroll = () => {
      const board = boardRef.current
      frame = 0
      if (!board || !dragging.current) return
      const { left, right } = board.getBoundingClientRect()
      const speed = last.x < left + EDGE_PX ? -12 : last.x > right - EDGE_PX ? 12 : 0
      if (!speed) return
      board.scrollLeft += speed
      const over = columnAt(last.x, last.y)
      setDrag((d) => (d && d.over !== over ? { ...d, over } : d))
      frame = requestAnimationFrame(autoScroll)
    }

    const onMoveEvent = (ev: PointerEvent) => {
      if (ev.pointerId !== pointerId) return
      if (!dragging.current) {
        if (Math.hypot(ev.clientX - start.x, ev.clientY - start.y) <= SLOP_PX) return
        if (pointerType === 'touch') return end()
        begin()
      }
      pos.current = { x: ev.clientX - offset.x, y: ev.clientY - offset.y }
      place()
      last.x = ev.clientX
      last.y = ev.clientY
      const over = columnAt(ev.clientX, ev.clientY)
      setDrag((d) => (d && d.over !== over ? { ...d, over } : d))
      if (!frame) frame = requestAnimationFrame(autoScroll)
    }
    const onUp = (ev: PointerEvent) => {
      if (ev.pointerId !== pointerId) return
      if (dragging.current) {
        const over = columnAt(ev.clientX, ev.clientY)
        if (over !== null && over !== from) onMove(issue, over)
      }
      end()
    }
    const onCancel = (ev: PointerEvent) => ev.pointerId === pointerId && end()

    function end() {
      clearTimeout(timer)
      cancelAnimationFrame(frame)
      window.removeEventListener('pointermove', onMoveEvent)
      window.removeEventListener('pointerup', onUp)
      window.removeEventListener('pointercancel', onCancel)
      if (dragging.current) setDrag(null)
      dragging.current = false
    }
    window.addEventListener('pointermove', onMoveEvent)
    window.addEventListener('pointerup', onUp)
    window.addEventListener('pointercancel', onCancel)
  }

  return (
    <div ref={boardRef} className="board" aria-label="Board" role="region">
      {groups.map((g) => (
        <section key={g.key} data-column={g.key} data-over={drag && drag.over === g.key ? '' : undefined} className="board-column" aria-label={g.label}>
          <header className="flex items-center gap-2 px-2">
            <GroupIcon group={g} by={by} />
            <span className="text-label ellipsis">{g.label}</span>
            <span className="text-caption text-secondary">{g.issues.length}</span>
          </header>
          <ul className="grid gap-2 m-0 p-0" style={{ listStyle: 'none' }}>
            {g.issues.map((issue) => (
              <li key={issue.id}>
                <Link
                  to="/$workspace/issue/$identifier"
                  params={{ workspace, identifier: issue.identifier }}
                  className="board-card"
                  draggable={false}
                  data-draggable={issue.can_edit ? '' : undefined}
                  data-dragging={drag?.issue.id === issue.id ? '' : undefined}
                  onPointerDown={(e) => press(e, issue, g.key)}
                  onContextMenu={(e) => dragging.current && e.preventDefault()}
                  onClick={(e) => {
                    if (!suppressClick.current || e.detail === 0) return
                    e.preventDefault()
                    suppressClick.current = false
                  }}
                >
                  <CardBody issue={issue} />
                </Link>
              </li>
            ))}
          </ul>
        </section>
      ))}
      {drag ? (
        <div
          aria-hidden
          className="board-card board-ghost"
          style={{ width: drag.width }}
          ref={(el) => {
            ghost.current = el
            place()
          }}
        >
          <CardBody issue={drag.issue} />
        </div>
      ) : null}
    </div>
  )
}
