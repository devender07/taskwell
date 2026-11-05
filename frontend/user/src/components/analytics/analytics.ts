import type { TeamMember } from '../../api.ts'

// A bar's height in percent of the tallest; any nonzero value stays visible.
export const barPercent = (value: number, max: number) => (max > 0 && value > 0 ? Math.max(4, Math.round((value / max) * 100)) : 0)

// "2026-08-17" (a Monday, UTC) as "Aug 17".
export const weekLabel = (week: string) =>
  new Date(`${week}T00:00:00Z`).toLocaleDateString('en-US', { month: 'short', day: 'numeric', timeZone: 'UTC' })

export const days = (n: number) => (n < 0.1 ? 'under a day' : `${n} ${n === 1 ? 'day' : 'days'}`)

export const TEAM_SORTS = ['name', 'open', 'overdue', 'completed_30d', 'cycle_time_days'] as const
export type TeamSort = (typeof TEAM_SORTS)[number]
export type TeamSearch = { sort?: Exclude<TeamSort, 'name'>; desc?: true }

export function validateTeamSearch(search: Record<string, unknown>): TeamSearch {
  const sort = TEAM_SORTS.find((s) => s === search.sort && s !== 'name') as TeamSearch['sort']
  return { ...(sort ? { sort } : {}), ...(search.desc === true ? { desc: true as const } : {}) }
}

// Members without a cycle time sort last either way; ties keep name order.
export function sortTeam(rows: TeamMember[], { sort = 'name', desc }: { sort?: TeamSort; desc?: boolean }) {
  const dir = desc ? -1 : 1
  const byName = (a: TeamMember, b: TeamMember) => a.name.localeCompare(b.name)
  return [...rows].sort((a, b) => {
    if (sort === 'name') return dir * byName(a, b)
    const [x, y] = [a[sort], b[sort]]
    if (x === null || y === null) return x === y ? byName(a, b) : x === null ? 1 : -1
    return dir * (x - y) || byName(a, b)
  })
}
