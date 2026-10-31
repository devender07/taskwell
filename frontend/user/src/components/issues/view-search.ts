import type { Issue, Member, Status } from '@/api'

export type GroupBy = 'status' | 'assignee' | 'priority' | 'none'
export type SortBy = 'priority' | 'updated' | 'created' | 'due'
export type FilterKey = 'status' | 'assignee' | 'label' | 'priority' | 'cycle'

// Defaults (list, status, priority, no filters) are left out of the URL.
// A lone priority stays a number: the router JSON-quotes digit-only strings (`"1"`), numbers stay bare.
export type ViewSearch = {
  view?: 'board'
  group?: Exclude<GroupBy, 'status'>
  sort?: Exclude<SortBy, 'priority'>
  status?: string
  assignee?: string
  label?: string
  priority?: string | number
  cycle?: string
}

export type IssueGroup = { key: string; label: string; issues: Issue[]; status?: Status }

const GROUPS = ['assignee', 'priority', 'none'] as const
const SORTS = ['updated', 'created', 'due'] as const
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
const VALID: Record<FilterKey, (v: string) => boolean> = {
  status: (v) => UUID.test(v),
  assignee: (v) => v === 'me' || v === 'none' || UUID.test(v),
  label: (v) => UUID.test(v),
  priority: (v) => /^[0-4]$/.test(v),
  cycle: (v) => v === 'none' || UUID.test(v),
}
export const FILTER_KEYS = Object.keys(VALID) as FilterKey[]

const pick = <T extends string>(list: readonly T[], v: unknown) => list.find((x) => x === v)

export function validateViewSearch(search: Record<string, unknown>): ViewSearch {
  const out: ViewSearch = {}
  if (search.view === 'board') out.view = 'board'
  const group = pick(GROUPS, search.group)
  if (group) out.group = group
  const sort = pick(SORTS, search.sort)
  if (sort) out.sort = sort
  for (const key of FILTER_KEYS) {
    const v = filterParam(filterValues(search, key).filter(VALID[key]))
    if (v !== undefined) (out as Record<FilterKey, unknown>)[key] = v
  }
  return out
}

export function filterValues(search: Record<string, unknown>, key: FilterKey): string[] {
  const v = search[key]
  if (typeof v !== 'string' && typeof v !== 'number') return []
  return [...new Set(String(v).split(',').filter(Boolean))]
}

export function filterParam(values: string[]) {
  if (values.length === 0) return undefined
  const v = values.join(',')
  return /^\d+$/.test(v) ? Number(v) : v
}

// OR within a filter, AND across filters.
export function filterIssues(issues: Issue[], search: ViewSearch, userId: string) {
  const status = filterValues(search, 'status')
  const assignee = filterValues(search, 'assignee')
  const label = filterValues(search, 'label')
  const priority = filterValues(search, 'priority')
  const cycle = filterValues(search, 'cycle')
  return issues.filter(
    (i) =>
      (!status.length || status.includes(i.status.id)) &&
      (!assignee.length ||
        assignee.some((a) => (a === 'none' ? !i.assignee : a === 'me' ? i.assignee?.id === userId : i.assignee?.id === a))) &&
      (!label.length || i.labels.some((l) => label.includes(l.id))) &&
      (!priority.length || priority.includes(String(i.priority))) &&
      (!cycle.length || cycle.includes(i.cycle?.id ?? 'none')),
  )
}

const time = (s: string) => Date.parse(s)
const priorityRank = (p: number) => (p === 0 ? 5 : p)

// Ties fall back to the newest issue number.
export function sortIssues(issues: Issue[], sort: SortBy) {
  const by: Record<SortBy, (a: Issue, b: Issue) => number> = {
    priority: (a, b) => priorityRank(a.priority) - priorityRank(b.priority),
    updated: (a, b) => time(b.updated_at) - time(a.updated_at),
    created: (a, b) => time(b.created_at) - time(a.created_at),
    due: (a, b) => (a.due_date ?? '9999').localeCompare(b.due_date ?? '9999'),
  }
  return [...issues].sort((a, b) => by[sort](a, b) || b.number - a.number)
}

export const PRIORITY_ORDER = [1, 2, 3, 4, 0]
const PRIORITY_NAMES = ['No priority', 'Urgent', 'High', 'Medium', 'Low']

// Every group in display order, empty ones included (the board shows them, the list hides them).
export function groupIssues(issues: Issue[], group: GroupBy, ctx: { statuses: Status[]; members: Member[]; userId: string }): IssueGroup[] {
  const of = (key: (i: Issue) => string) => (k: string) => issues.filter((i) => key(i) === k)
  if (group === 'status') {
    const take = of((i) => i.status.id)
    return [...ctx.statuses].sort((a, b) => a.position - b.position).map((s) => ({ key: s.id, label: s.name, status: s, issues: take(s.id) }))
  }
  if (group === 'priority') {
    const take = of((i) => String(i.priority))
    return PRIORITY_ORDER.map((p) => ({ key: String(p), label: PRIORITY_NAMES[p]!, issues: take(String(p)) }))
  }
  if (group === 'assignee') {
    const take = of((i) => i.assignee?.id ?? '')
    const people = new Map(ctx.members.map((m) => [m.id, m.name]))
    for (const i of issues) if (i.assignee && !people.has(i.assignee.id)) people.set(i.assignee.id, i.assignee.name)
    const ordered = [...people].sort(([a, an], [b, bn]) => Number(b === ctx.userId) - Number(a === ctx.userId) || an.localeCompare(bn))
    return [...ordered.map(([id, name]) => ({ key: id, label: name, issues: take(id) })), { key: '', label: 'No assignee', issues: take('') }]
  }
  return [{ key: 'all', label: 'All issues', issues }]
}
