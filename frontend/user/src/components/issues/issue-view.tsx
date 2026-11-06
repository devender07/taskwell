import { Link, useNavigate, useRouter } from '@tanstack/react-router'
import {
  memo,
  startTransition,
  useActionState,
  useEffect,
  useEffectEvent,
  useLayoutEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
  type MouseEvent as ReactMouseEvent,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
} from 'react'
import { Avatar } from '@taskwell/shared/avatar'
import { Button } from '@taskwell/shared/button'
import { Input } from '@taskwell/shared/input'
import { Checkbox } from '@taskwell/shared/checkbox'
import { Popover } from '@taskwell/shared/popover'
import { SmartAutocomplete, type Option } from '@taskwell/shared/smart-autocomplete'
import { Tabs } from '@taskwell/shared/tabs'
import { ChevronDownIcon, ChevronRightIcon } from '@taskwell/shared/icons'
import { api, issuePath, avatarSrc, toFormErrors, type CycleRef, type Issue, type Label, type Status, type Workspace } from '@/api'
import { reloadPage } from '@/realtime'
import { FormError } from '@/components/auth/auth-card'
import { AssignedBy, LabelDot, StatusIcon, formatDate, overdue, priorityLabel } from './fields'
import {
  FILTER_KEYS,
  PRIORITY_ORDER,
  applyOverrides,
  filterParam,
  filterValues,
  groupKey,
  issuePageQuery,
  neighbours,
  orderGroups,
  pageAssignees,
  viewGroup,
  type FilterKey,
  type GroupBy,
  type IssueGroup,
  type IssuePage,
  type Neighbours,
  type Override,
  type SortBy,
  type ViewSearch,
} from './view-search'
import { useIssuePages, usePageEnd } from './issue-pages'
import { BulkBar } from './bulk-bar'
import { singleKeyShortcuts } from '@/components/app/shortcuts'
import { ViewMenu } from './saved-views'
import { dropMark, slotAt, useIssueDrag } from './drag'
import './issues.css'
import { byId, extractDataFromArray } from '@taskwell/shared/list'

type Props = {
  workspace: Workspace
  page: IssuePage
  source: string
  statuses: Status[]
  labels: Label[]
  userId: string
  search: ViewSearch
  mine?: boolean
  cycles?: CycleRef[]
  defaultSort?: SortBy
  manual?: boolean
}
export type Choice = { key: FilterKey; title: string; options: { value: string; label: string; icon?: ReactNode }[] }
type Move = { issue: Issue; path: string; form: FormData; override: Override }
const views = [
  { value: 'list', label: 'List' },
  { value: 'board', label: 'Board' },
] as const
const sortOptions: Option[] = [
  { value: 'manual', label: 'Manual' },
  { value: 'priority', label: 'Priority' },
  { value: 'updated', label: 'Last updated' },
  { value: 'created', label: 'Created' },
  { value: 'due', label: 'Due date' },
]

// Toolbar plus list or board for the issues at `source`, starting from its first page (`page`, per group); display
// options and filters live in the URL and the server applies them.
export function IssueView({ workspace, page, source, statuses, labels, userId, search, mine, cycles, defaultSort = 'priority', manual }: Props) {
  const navigate = useNavigate()
  const router = useRouter()
  const [overrides, setOverrides] = useState<Override[]>([])
  const [announcement, setAnnouncement] = useState('')
  const root = useRef<HTMLDivElement>(null)
  const refocus = useRef<{ id: string; group: string; until: number } | null>(null)
  const hint = useId()
  const [seen, setSeen] = useState(page)
  if (page !== seen) {
    setSeen(page)
    // Kept until fresh data has the move, so the card never shows in its old column in between. A settled move gives
    // way to the data (which may have it further down its new column, or somewhere else if someone moved it meanwhile).
    const loaded = page.groups.flatMap((g) => g.issues)
    // A manual place is kept until its own reload, since the data can't show whether it has it yet.
    const left = overrides.filter(
      (o) => !o.settled && (o.after !== undefined || !loaded.some((i) => i.id === o.issue.id && groupKey(i, o.by) === o.key)),
    )
    if (left.length !== overrides.length) setOverrides(left)
  }
  const [error, move] = useActionState(async (_prev: string | null, { path, form, override }: Move) => {
    const drop = () => setOverrides((list) => list.filter((o) => o !== override))
    try {
      await api<Issue>(path, form)
    } catch (e) {
      drop()
      const { errors, error } = toFormErrors(e)
      return Object.values(errors).find(Boolean) ?? error
    }
    await reloadPage(router)
    override.settled = true
    if (override.after !== undefined) drop()
    return null
  }, null)

  // A keyboard move re-mounts the issue (more than once: optimistic, then saved); focus follows it for a few seconds,
  // unless the user has moved focus elsewhere.
  useEffect(() => {
    const want = refocus.current
    if (!want) return
    if (Date.now() > want.until) return void (refocus.current = null)
    const el = root.current?.querySelector<HTMLElement>(`[data-group="${CSS.escape(want.group)}"] [data-issue="${CSS.escape(want.id)}"]`)
    const lost = !document.activeElement || document.activeElement === document.body
    if (el && lost) el.focus()
  })

  const view = search.view ?? 'list'
  const group = viewGroup(search, mine)
  const sort: SortBy = search.sort === 'manual' && !manual ? 'priority' : (search.sort ?? defaultSort)
  const ordered = sort === 'manual'
  const pages = useIssuePages(page, source, issuePageQuery(search, mine, defaultSort))
  const groups = useMemo(
    () => applyOverrides(orderGroups(pages.page, group, { statuses, userId }), overrides, group),
    [pages.page, group, statuses, userId, overrides],
  )
  const assignees = useMemo(() => pageAssignees(pages.page), [pages.page])
  const setSearch = (next: ViewSearch) => navigate({ to: '.', search: next })

  // Selected list rows by issue id; a new page, view or filter starts over.
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set())
  const lastPicked = useRef<string | null>(null)
  const selectionKey = `${source}?${issuePageQuery(search, mine, defaultSort)}&view=${view}`
  const [seenKey, setSeenKey] = useState(selectionKey)
  if (selectionKey !== seenKey) {
    setSeenKey(selectionKey)
    setSelected(new Set())
  }
  const loaded = groups.flatMap((g) => g.issues)
  const picked = loaded.filter((i) => selected.has(i.id))
  const clearSelection = () => setSelected(new Set())

  // Shift picks the range from the last row picked, to the same state as this one.
  function pick(issue: Issue, range: boolean) {
    const from = range && lastPicked.current ? loaded.findIndex((i) => i.id === lastPicked.current) : -1
    const to = loaded.findIndex((i) => i.id === issue.id)
    const rows = from < 0 ? [issue] : loaded.slice(Math.min(from, to), Math.max(from, to) + 1).filter((i) => i.can_edit)
    const on = !selected.has(issue.id)
    const next = new Set(selected)
    for (const i of rows) if (on) next.add(i.id); else next.delete(i.id)
    setSelected(next)
    lastPicked.current = issue.id
  }

  // X toggles the focused row, Escape clears the selection; not while typing, in a dialog, or once a popover took it.
  const onSelectKey = useEffectEvent((e: KeyboardEvent) => {
    if (view !== 'list' || e.defaultPrevented || e.metaKey || e.ctrlKey || e.altKey || document.querySelector('dialog[open]')) return
    if (e.key === 'Escape' && selected.size) return clearSelection()
    if (e.key.toLowerCase() !== 'x' || !singleKeyShortcuts() || !(e.target instanceof Element)) return
    const id = e.target.closest<HTMLElement>('[data-issue]')?.dataset.issue
    const issue = id && root.current?.contains(e.target) ? byId(loaded, id) : undefined
    if (!issue?.can_edit) return
    e.preventDefault()
    pick(issue, e.shiftKey)
  })
  useEffect(() => {
    window.addEventListener('keydown', onSelectKey)
    return () => window.removeEventListener('keydown', onSelectKey)
  }, [])

  // The toolbar sticks under the page's own sticky header, and list group headers under the toolbar (--below-toolbar).
  const toolbar = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => {
    const bar = toolbar.current
    const head = bar?.closest('main')?.querySelector<HTMLElement>(':scope > .page-top')
    if (!bar || !head) return
    const fit = () => {
      const top = parseFloat(getComputedStyle(head).top) + head.offsetHeight
      bar.style.top = `${top}px`
      root.current?.style.setProperty('--below-toolbar', `${top + bar.offsetHeight}px`)
    }
    fit()
    const ro = new ResizeObserver(fit)
    ro.observe(head)
    ro.observe(bar)
    return () => ro.disconnect()
  }, [])

  const choices: Choice[] = [
    { key: 'status', title: 'Status', options: statuses.map((s) => ({ value: s.id, label: s.name, icon: <StatusIcon status={s} /> })) },
    ...(mine
      ? []
      : [
          {
            key: 'assignee' as const,
            title: 'Assignee',
            options: [
              { value: 'me', label: 'Me' },
              { value: 'none', label: 'No assignee' },
              ...[...assignees].filter(([id]) => id !== userId).map(([id, name]) => ({ value: id, label: name })),
            ],
          },
        ]),
    { key: 'priority', title: 'Priority', options: PRIORITY_ORDER.map((p) => ({ value: String(p), label: priorityLabel(p) })) },
    { key: 'label', title: 'Label', options: labels.map((l) => ({ value: l.id, label: l.name, icon: <LabelDot color={l.color} /> })) },
    ...(cycles
      ? [{ key: 'cycle' as const, title: 'Cycle', options: [{ value: 'none', label: 'No cycle' }, ...cycles.map((c) => ({ value: c.id, label: c.name }))] }]
      : []),
  ]
  const active = choices
    .map((c) => ({ ...c, values: filterValues(search, c.key).filter((v) => c.options.some((o) => o.value === v)) }))
    .filter((c) => c.values.length > 0)

  const setFilter = (key: FilterKey, values: string[]) => setSearch({ ...search, [key]: filterParam(values) })
  const clearFilters = () => setSearch(Object.fromEntries(Object.entries(search).filter(([k]) => !FILTER_KEYS.includes(k as FilterKey))))

  const groupOptions: Option[] = [
    { value: 'status', label: 'Status' },
    ...(mine ? [] : [{ value: 'assignee', label: 'Assignee' }]),
    { value: 'priority', label: 'Priority' },
    ...(view === 'list' ? [{ value: 'none', label: 'No grouping' }] : []),
  ]

  // Saves a move to group `key`: its field through the update endpoint, or with neighbours through the move endpoint,
  // which also changes the field when the group differs.
  function onMove(issue: Issue, key: string, at?: Neighbours) {
    const form = new FormData()
    let next: Partial<Issue> = {}
    if (key === groupKey(issue, group)) {
      if (!at) return
    } else if (group === 'status') {
      const status = byId(statuses, key)
      if (!status) return
      form.set('status_id', key)
      next = { status }
    } else if (group === 'assignee') {
      const name = groups.find((g) => g.key === key)?.label
      form.set('assignee_id', key === 'none' ? '' : key)
      next = { assignee: key !== 'none' && name ? { id: key, name } : null }
    } else if (group === 'priority') {
      form.set('priority', key)
      next = { priority: Number(key) }
    }
    let path = issuePath(workspace, issue.identifier)
    if (at) {
      path += '/move'
      form.set('after', extractDataFromArray(at.after, 'identifier', ''))
      form.set('before', extractDataFromArray(at.before, 'identifier', ''))
    }
    const override: Override = { issue: { ...issue, ...next }, next, key, by: group, settled: false, ...(at ? { after: extractDataFromArray(at.after, 'id', null) } : {}) }
    setOverrides((list) => [...list.filter((o) => o.issue.id !== issue.id), override])
    startTransition(() => move({ issue, path, form, override }))
  }

  // Puts an issue at slot `index` among group `key`'s other rendered issues; into an empty group it only changes group.
  function onPlace(issue: Issue, key: string, index: number) {
    const at = neighbours(extractDataFromArray(groups.find((g) => g.key === key), 'issues', []), issue, index)
    if (at) onMove(issue, key, at.after || at.before ? at : undefined)
  }

  function keyMove(e: ReactKeyboardEvent<HTMLElement>, issue: Issue, from: string) {
    if (e.ctrlKey || e.metaKey || !issue.can_edit) return
    const g = groups.find((g) => g.key === from)
    if (ordered && e.altKey && !e.shiftKey && (e.key === 'ArrowUp' || e.key === 'ArrowDown') && g) {
      e.preventDefault()
      const up = e.key === 'ArrowUp'
      const to = g.issues.findIndex((i) => i.id === issue.id) + (up ? -1 : 1)
      if (to < 0 || to >= g.issues.length) return setAnnouncement(`${issue.identifier} is already ${up ? 'first' : 'last'}.`)
      refocus.current = { id: issue.id, group: from, until: Date.now() + 3000 }
      onPlace(issue, from, to)
      setAnnouncement(`Moved ${issue.identifier} to position ${to + 1} of ${g.count}.`)
      return
    }
    if (view !== 'board' || !e.shiftKey || e.altKey || (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight')) return
    e.preventDefault()
    const at = groups.findIndex((g) => g.key === from)
    const to = groups[at + (e.key === 'ArrowLeft' ? -1 : 1)]
    if (!to) return setAnnouncement(`${issue.identifier} is already in the ${e.key === 'ArrowLeft' ? 'first' : 'last'} column.`)
    refocus.current = { id: issue.id, group: to.key, until: Date.now() + 3000 }
    onMove(issue, to.key)
    setAnnouncement(`Moved ${issue.identifier} to ${to.label}.`)
  }

  return (
    <div ref={root} className="grid gap-4" style={picked.length ? { paddingBottom: 96 } : undefined}>
      <div ref={toolbar} className="page-top issue-toolbar grid gap-2">
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
            options={manual ? sortOptions : sortOptions.slice(1)}
            value={sort}
            onChange={(v) => setSearch({ ...search, sort: v === defaultSort ? undefined : (v as SortBy) })}
          />
          <div style={{ width: 220 }}>
            <Input
              type="search"
              aria-label="Search issues"
              placeholder="Search issues"
              defaultValue={search.q}
              onDebouncedChange={(v) => navigate({ to: '.', search: { ...search, q: v.trim() || undefined }, replace: true })}
            />
          </div>
          <FilterMenu choices={choices} search={search} onChange={setFilter} />
          <ViewMenu workspace={workspace} />
          <span className="text-caption text-secondary" style={{ marginLeft: 'auto' }}>
            {page.total} {page.total === 1 ? 'issue' : 'issues'}
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
      </div>
      <FormError>{error}</FormError>
      {view === 'board' ? (
        <Board
          workspace={workspace.slug}
          groups={groups}
          by={group}
          ordered={ordered}
          hint={hint}
          onMove={onMove}
          onPlace={onPlace}
          onKey={keyMove}
          more={pages.more}
          errorOf={pages.error}
        />
      ) : page.total === 0 ? (
        <p className="text-body-sm text-secondary p-6 bg-surface border rounded-lg text-center">No issues match these filters.</p>
      ) : (
        <IssueList
          workspace={workspace.slug}
          groups={groups}
          by={group}
          ordered={ordered}
          hint={hint}
          onPlace={onPlace}
          onKey={keyMove}
          more={pages.more}
          errorOf={pages.error}
          selected={selected}
          onPick={pick}
        />
      )}
      {view === 'list' && picked.length ? (
        <BulkBar
          workspace={workspace}
          issues={picked}
          choices={choices}
          onClear={clearSelection}
          onDone={(message) => {
            clearSelection()
            setAnnouncement(message)
          }}
        />
      ) : null}
      <p id={hint} className="sr-only">
        {view === 'board' ? 'Press Shift+Left or Shift+Right to move to another column. ' : ''}
        {ordered ? 'Press Alt+Up or Alt+Down to move up or down.' : ''}
      </p>
      <p className="sr-only" role="status">{announcement}</p>
    </div>
  )
}

type CardHandlers = {
  press: (e: ReactPointerEvent<HTMLElement>, issue: Issue, from: string) => void
  key: (e: ReactKeyboardEvent<HTMLElement>, issue: Issue, from: string) => void
  click: (e: ReactMouseEvent<HTMLElement>) => void
  contextMenu: (e: ReactMouseEvent<HTMLElement>) => void
}
type ViewProps = {
  workspace: string
  groups: IssueGroup[]
  by: GroupBy
  ordered: boolean
  hint: string
  onPlace: (issue: Issue, key: string, index: number) => void
  onKey: CardHandlers['key']
  more: (key: string) => void
  errorOf: (key: string) => string | undefined
}

// Drag and keyboard handlers that stay the same across renders, so memoized cards and rows skip a drag's re-renders.
function useHandlers(drag: Omit<CardHandlers, 'key'>, onKey: CardHandlers['key']) {
  const key = useRef(onKey)
  key.current = onKey
  return useMemo<CardHandlers>(() => ({ ...drag, key: (e, issue, from) => key.current(e, issue, from) }), [drag.press, drag.click, drag.contextMenu])
}

// With manual order, rows drag within their group; a drop line marks the slot.
type ListProps = ViewProps & { selected: ReadonlySet<string>; onPick: (issue: Issue, range: boolean) => void }

function IssueList({ workspace, groups, by, ordered, hint, onPlace, onKey, more, errorOf, selected, onPick }: ListProps) {
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set())
  const list = useRef<HTMLDivElement>(null)
  const { drag, ghostRef, ...dragHandlers } = useIssueDrag({
    targetAt: (_x, y, issue, from) => {
      const section = list.current?.querySelector(`[data-group="${CSS.escape(from)}"]`)
      return section ? { group: from, index: slotAt(section, y, issue.id) } : null
    },
    onDrop: (issue, _from, at) => at.index !== null && onPlace(issue, at.group, at.index),
  })
  const on = useHandlers(dragHandlers, onKey)

  return (
    <div ref={list} className="grid gap-4" data-selecting={selected.size ? '' : undefined}>
      {groups
        .filter((g) => g.count > 0 || g.issues.length > 0)
        .map((g) => {
          const open = by === 'none' || !collapsed.has(`${by}:${g.key}`)
          const mark = dropMark(g.issues, drag, g.key)
          return (
            <section key={g.key} data-group={g.key} className="issue-group bg-surface border rounded-lg" aria-label={by === 'none' ? 'Issues' : g.label}>
              {by === 'none' ? null : (
                <h2 className="m-0">
                  <button
                    type="button"
                    className="issue-group-header issue-group-toggle"
                    aria-expanded={open}
                    onClick={() =>
                      setCollapsed((prev) => {
                        const next = new Set(prev)
                        const key = `${by}:${g.key}`
                        if (!next.delete(key)) next.add(key)
                        return next
                      })
                    }
                  >
                    {open ? <ChevronDownIcon size={16} /> : <ChevronRightIcon size={16} />}
                    <GroupIcon group={g} by={by} />
                    <span className="text-label">{g.label}</span>
                    <span className="text-caption text-secondary">{g.count}</span>
                  </button>
                </h2>
              )}
              {open ? (
                <ul className="grid m-0 p-0" style={{ listStyle: 'none' }}>
                  {g.issues.map((issue) => (
                    <li key={issue.id} className="issue-item" data-drop={mark?.id === issue.id ? mark.side : undefined}>
                      <Checkbox
                        className="issue-select"
                        label={<span className="sr-only">Select {issue.identifier}</span>}
                        checked={selected.has(issue.id)}
                        disabled={!issue.can_edit}
                        onChange={(e) => onPick(issue, (e.nativeEvent as MouseEvent).shiftKey === true)}
                      />
                      {ordered ? (
                        <IssueRow workspace={workspace} issue={issue} group={g.key} dragging={drag?.issue.id === issue.id} hint={hint} on={on} />
                      ) : (
                        <IssueRow workspace={workspace} issue={issue} />
                      )}
                    </li>
                  ))}
                  <GroupEnd group={g} more={more} error={errorOf(g.key)} />
                </ul>
              ) : null}
            </section>
          )
        })}
      {drag ? (
        <div aria-hidden className="issue-row board-ghost" style={{ width: drag.width }} ref={ghostRef}>
          <span className="issue-id flex-noshrink">{drag.issue.identifier}</span>
          <span className="text-label text-primary ellipsis">{drag.issue.title}</span>
        </div>
      ) : null}
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

// The end of a group while it has more: loads the next page as it scrolls into view (the board's column scrolls on its
// own, the list with the window).
function GroupEnd({ group, more, error, scroller }: { group: IssueGroup; more: (key: string) => void; error?: string; scroller?: boolean }) {
  const ref = usePageEnd<HTMLLIElement>(group.has_more && !error ? more : null, group.key, group.issues.length, scroller)
  if (error) return <li role="alert" className="page-end text-caption text-accent">{error}</li>
  if (!group.has_more) return null
  return <li ref={ref} className="page-end text-caption text-secondary">Loading more…</li>
}

function GroupIcon({ group, by }: { group: IssueGroup; by: GroupBy }) {
  if (group.status) return <StatusIcon status={group.status} />
  if (by === 'assignee' && group.key !== 'none') return <Avatar name={group.label} size={20} src={avatarSrc(group.key)} />
  return null
}

type IssueRowProps = { workspace: string; issue: Issue; group?: string; dragging?: boolean; hint?: string; on?: CardHandlers }

export const IssueRow = memo(function IssueRow({ workspace, issue, group = '', dragging, hint, on }: IssueRowProps) {
  const movable = on && issue.can_edit
  return (
    <Link
      to="/$workspace/issue/$identifier"
      params={{ workspace, identifier: issue.identifier }}
      className="issue-row"
      draggable={on ? false : undefined}
      data-issue={issue.id}
      data-draggable={movable ? '' : undefined}
      data-dragging={dragging ? '' : undefined}
      aria-describedby={movable ? hint : undefined}
      onPointerDown={on && ((e) => on.press(e, issue, group))}
      onKeyDown={on && ((e) => on.key(e, issue, group))}
      onContextMenu={on?.contextMenu}
      onClick={on?.click}
    >
      <StatusIcon status={issue.status} label={issue.status.name} />
      <span className="issue-id flex-noshrink" style={{ minWidth: 64 }}>{issue.identifier}</span>
      {issue.parent ? <span className="issue-id flex-noshrink">↳ {issue.parent.identifier}</span> : null}
      <span className="text-label text-primary ellipsis flex-grow">{issue.title}</span>
      <LabelDots issue={issue} />
      <span className="issue-row-wide text-caption text-secondary flex-noshrink">{issue.status.name}</span>
      {issue.priority ? <span className="text-caption text-secondary flex-noshrink">{priorityLabel(issue.priority)}</span> : null}
      <DueDate issue={issue} />
      <span className="issue-row-wide flex-noshrink ellipsis" style={{ maxWidth: 160 }}>
        <AssignedBy issue={issue} />
      </span>
      <Assignee issue={issue} />
    </Link>
  )
})

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
    <span role="img" aria-label={`Assigned to ${issue.assignee.name}`} title={issue.assigned_by ? `${issue.assignee.name}, assigned by ${issue.assigned_by.name}` : issue.assignee.name} className="inline-grid flex-noshrink">
      <Avatar name={issue.assignee.name} size={24} src={avatarSrc(issue.assignee.id)} />
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
      <AssignedBy issue={issue} />
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


// Columns are drop targets ([data-group]); with manual order a drop line marks the slot in the column, otherwise only
// the column changes. From the keyboard, Shift+←/→ on a focused card moves it to the next column.
function Board({ workspace, groups, by, ordered, hint, onMove, onPlace, onKey, more, errorOf }: ViewProps & { onMove: (issue: Issue, key: string) => void }) {
  const boardRef = useRef<HTMLDivElement>(null)
  const { drag, ghostRef, ...dragHandlers } = useIssueDrag({
    scroller: boardRef,
    targetAt: (x, y, issue) => {
      const column = document.elementFromPoint(x, y)?.closest<HTMLElement>('[data-group]')
      const group = column?.dataset.group
      if (!column || group === undefined) return null
      return { group, index: ordered ? slotAt(column, y, issue.id) : null }
    },
    onDrop: (issue, from, at) => (at.index === null ? at.group !== from && onMove(issue, at.group) : onPlace(issue, at.group, at.index)),
  })
  const on = useHandlers(dragHandlers, onKey)

  // The board runs to the bottom of the window, so its horizontal scrollbar sits there and each column scrolls on its
  // own. It's re-measured when the window or the content above it (filters, chips) changes size.
  useLayoutEffect(() => {
    const board = boardRef.current
    const above = board?.parentElement
    if (!board || !above) return
    const fit = () => {
      const top = board.getBoundingClientRect().top + window.scrollY
      const main = board.closest('main')
      const gap = main ? parseFloat(getComputedStyle(main).paddingBottom) : 24
      board.style.height = `min(700px, max(600px, calc(100dvh - ${Math.round(top + gap)}px)))`
    }
    fit()
    const ro = new ResizeObserver(fit)
    ro.observe(above)
    window.addEventListener('resize', fit)
    return () => {
      ro.disconnect()
      window.removeEventListener('resize', fit)
    }
  }, [])

  return (
    <div ref={boardRef} className="board" aria-label="Board" role="region">
      {groups.map((g) => {
        const mark = dropMark(g.issues, drag, g.key)
        return (
          <section key={g.key} data-group={g.key} data-over={drag?.at?.group === g.key ? '' : undefined} className="board-column" aria-label={g.label}>
            <header className="flex items-center gap-2 px-2">
              <GroupIcon group={g} by={by} />
              <span className="text-label ellipsis">{g.label}</span>
              <span className="text-caption text-secondary">{g.count}</span>
            </header>
            <ul className="board-cards">
              {g.issues.map((issue) => (
                <li key={issue.id} data-drop={mark?.id === issue.id ? mark.side : undefined}>
                  <BoardCard workspace={workspace} issue={issue} column={g.key} dragging={drag?.issue.id === issue.id} hint={hint} on={on} />
                </li>
              ))}
              <GroupEnd group={g} more={more} error={errorOf(g.key)} scroller />
            </ul>
          </section>
        )
      })}
      {drag ? (
        <div aria-hidden className="board-card board-ghost" style={{ width: drag.width }} ref={ghostRef}>
          <CardBody issue={drag.issue} />
        </div>
      ) : null}
    </div>
  )
}

type BoardCardProps = { workspace: string; issue: Issue; column: string; dragging: boolean; hint: string; on: CardHandlers }

const BoardCard = memo(function BoardCard({ workspace, issue, column, dragging, hint, on }: BoardCardProps) {
  return (
    <Link
      to="/$workspace/issue/$identifier"
      params={{ workspace, identifier: issue.identifier }}
      className="board-card"
      draggable={false}
      data-issue={issue.id}
      data-draggable={issue.can_edit ? '' : undefined}
      data-dragging={dragging ? '' : undefined}
      aria-describedby={issue.can_edit ? hint : undefined}
      onPointerDown={(e) => on.press(e, issue, column)}
      onKeyDown={(e) => on.key(e, issue, column)}
      onContextMenu={on.contextMenu}
      onClick={on.click}
    >
      <CardBody issue={issue} />
    </Link>
  )
})
