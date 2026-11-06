import type { Issue, Status } from '@/api'
import { byId, extractDataFromArray } from '@taskwell/shared/list'

export type GroupBy = 'status' | 'assignee' | 'priority' | 'none'
export type SortBy = 'manual' | 'priority' | 'updated' | 'created' | 'due'
export type FilterKey = 'status' | 'assignee' | 'label' | 'priority' | 'cycle'

// Defaults (list, status, the page's default sort, no filters) are left out of the URL.
// A lone priority stays a number: the router JSON-quotes digit-only strings (`"1"`), numbers stay bare.
export type ViewSearch = {
  view?: 'board'
  group?: Exclude<GroupBy, 'status'>
  sort?: SortBy
  status?: string
  assignee?: string
  label?: string
  priority?: string | number
  cycle?: string
  q?: string // text search: an identifier, or words in the title or description
}

export type PageGroup = { key: string; count: number; issues: Issue[]; has_more: boolean }
export type IssuePage = { total: number; groups: PageGroup[] }
export type IssueGroup = PageGroup & { label: string; status?: Status }

const GROUPS = ['assignee', 'priority', 'none'] as const
const SORTS = ['manual', 'priority', 'updated', 'created', 'due'] as const
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
  const q = typeof search.q === 'string' || typeof search.q === 'number' ? String(search.q).trim().slice(0, 200) : ''
  if (q) out.q = q
  return out
}

export function filterValues(search: Record<string, unknown>, key: FilterKey): string[] {
  const v = search[key]
  if (typeof v !== 'string' && typeof v !== 'number') return []
  return [...new Set(String(v).split(',').filter(Boolean))]
}

// The group the view shows: the board never ungrouped, My issues never by assignee.
export function viewGroup(search: ViewSearch, mine?: boolean): GroupBy {
  const wanted = search.group ?? 'status'
  const allowed = mine && wanted === 'assignee' ? 'status' : wanted
  return search.view === 'board' && allowed === 'none' ? 'status' : allowed
}

// Project and cycle pages sort by hand once anyone has ordered the project; everywhere else by priority.
export const defaultSort = (project?: { manual_order: boolean }): SortBy => (project?.manual_order ? 'manual' : 'priority')

// First-page query for the issue endpoints: 50 per group, the view's group, sort (else the page's default) and
// filters. My issues ignores the assignee and cycle filters (it has neither).
export function issuePageQuery(search: ViewSearch, mine?: boolean, fallback: SortBy = 'priority') {
  const q = new URLSearchParams({ limit: '50', group: viewGroup(search, mine), sort: search.sort ?? fallback })
  for (const key of pageFilters(mine)) if (search[key] !== undefined) q.set(key, String(search[key]))
  if (search.q) q.set('q', search.q)
  return q.toString()
}

const pageFilters = (mine?: boolean) => (mine ? FILTER_KEYS.filter((k) => k !== 'assignee' && k !== 'cycle') : FILTER_KEYS)

export const hasFilters = (search: ViewSearch, mine?: boolean) => !!search.q || pageFilters(mine).some((k) => search[k] !== undefined)

export function filterParam(values: string[]) {
  if (values.length === 0) return undefined
  const v = values.join(',')
  return /^\d+$/.test(v) ? Number(v) : v
}

// The assignees of the page's loaded issues, by id. Lists name people from their issues rather than loading every member.
export function pageAssignees(page: IssuePage) {
  const people = new Map<string, string>()
  for (const g of page.groups) for (const i of g.issues) if (i.assignee) people.set(i.assignee.id, i.assignee.name)
  return people
}

export const PRIORITY_ORDER = [1, 2, 3, 4, 0]
const PRIORITY_NAMES = ['No priority', 'Urgent', 'High', 'Medium', 'Low']

// Every group in display order with the server's first page (or more) of it; empty groups get count 0 (the board shows
// them, the list hides them).
export function orderGroups(page: IssuePage, group: GroupBy, ctx: { statuses: Status[]; userId: string }): IssueGroup[] {
  const byKey = new Map(page.groups.map((g) => [g.key, g]))
  const fill = (key: string, label: string, status?: Status): IssueGroup => ({
    count: 0,
    issues: [],
    has_more: false,
    ...byKey.get(key),
    key,
    label,
    ...(status ? { status } : {}),
  })
  if (group === 'status') return [...ctx.statuses].sort((a, b) => a.position - b.position).map((s) => fill(s.id, s.name, s))
  if (group === 'priority') return PRIORITY_ORDER.map((p) => fill(String(p), PRIORITY_NAMES[p]!))
  if (group === 'assignee') {
    const people = pageAssignees(page)
    const ordered = [...people].sort(([a, an], [b, bn]) => Number(b === ctx.userId) - Number(a === ctx.userId) || an.localeCompare(bn))
    return [...ordered.map(([id, name]) => fill(id, name)), fill('none', 'No assignee')]
  }
  return [fill('all', 'All issues')]
}

// A move shown before the loader data has it; `settled` once the save and its reload finished. `after` places it by
// hand: right after that issue id, or first when null.
export type Override = { issue: Issue; next: Partial<Issue>; key: string; by: GroupBy; settled: boolean; after?: string | null }
export type Neighbours = { after?: Issue; before?: Issue }

export const groupKey = (i: Issue, by: GroupBy) =>
  by === 'status' ? i.status.id : by === 'assignee' ? (extractDataFromArray(i.assignee, 'id', 'none')) : by === 'priority' ? String(i.priority) : 'all'

function insertAfter(list: Issue[], issue: Issue, after: string | null | undefined) {
  const at = after ? list.findIndex((i) => i.id === after) + 1 : 0
  return [...list.slice(0, at), issue, ...list.slice(at)]
}

// Moves each overridden card into its new group, at its manual place or else the top, unless that group has loaded it
// and it has no manual place (then it's patched in place). Counts follow a card that moved between loaded groups; one
// not loaded anywhere is already counted there.
export function applyOverrides(groups: IssueGroup[], overrides: Override[], by: GroupBy) {
  let out = groups
  for (const o of overrides) {
    if (o.by !== by) continue
    const id = o.issue.id
    const from = out.find((g) => g.issues.some((i) => i.id === id))
    if (from?.key === o.key && o.after === undefined) {
      out = out.map((g) => (g === from ? { ...g, issues: g.issues.map((i) => (i.id === id ? { ...i, ...o.next } : i)) } : g))
      continue
    }
    const issue = { ...(byId(from?.issues, id) ?? o.issue), ...o.next }
    const moved = from && from.key !== o.key
    out = out.map((g) => {
      const issues = g === from ? g.issues.filter((i) => i.id !== id) : g.issues
      if (g.key === o.key) return { ...g, count: g.count + (moved ? 1 : 0), issues: insertAfter(issues, issue, o.after) }
      return g === from ? { ...g, count: g.count - 1, issues } : g
    })
  }
  return out
}

// The neighbours for putting `issue` at slot `index` among the other issues of `list` (its rendered group), or null
// when that's where it already is.
export function neighbours(list: Issue[], issue: Issue, index: number): Neighbours | null {
  if (list.findIndex((i) => i.id === issue.id) === index) return null
  const rest = list.filter((i) => i.id !== issue.id)
  return { after: rest[index - 1], before: rest[index] }
}
