import type { Issue } from '@/api'

export type Recent = { identifier: string; title: string }

const DAY = 86_400_000
export const localDate = (d: Date) => d.toLocaleDateString('en-CA')
const rank = (p: number) => (p === 0 ? 5 : p)
const open = (i: Issue) => i.status.category !== 'completed' && i.status.category !== 'canceled'

// My open issues split into those due within a week (or overdue), by due date then priority, and the rest.
export function splitMine(issues: Issue[], now = new Date()) {
  const week = localDate(new Date(now.getTime() + 7 * DAY))
  const due = (i: Issue) => i.due_date !== null && i.due_date <= week
  const mine = issues.filter(open)
  const attention = mine
    .filter(due)
    .sort((a, b) => a.due_date!.localeCompare(b.due_date!) || rank(a.priority) - rank(b.priority) || b.number - a.number)
  const rest = mine.filter((i) => !due(i)).sort((a, b) => rank(a.priority) - rank(b.priority) || b.number - a.number)
  return { attention, rest }
}

export function greeting(now = new Date()) {
  const h = now.getHours()
  return h >= 5 && h < 12 ? 'Good morning' : h >= 12 && h < 17 ? 'Good afternoon' : 'Good evening'
}

// Newest first, one entry per issue, at most 20.
export const pushRecent = (list: Recent[], item: Recent) => [item, ...list.filter((r) => r.identifier !== item.identifier)].slice(0, 20)

const key = (workspace: string) => `taskwell:recent:${workspace}`

export function readRecent(workspace: string): Recent[] {
  try {
    const v = JSON.parse(localStorage.getItem(key(workspace)) ?? '[]')
    return Array.isArray(v) ? v.filter((r) => typeof r?.identifier === 'string' && typeof r?.title === 'string') : []
  } catch {
    return []
  }
}

function writeRecent(workspace: string, list: Recent[]) {
  try {
    localStorage.setItem(key(workspace), JSON.stringify(list))
  } catch {}
}

export const recordRecent = (workspace: string, item: Recent) => writeRecent(workspace, pushRecent(readRecent(workspace), item))

export function pruneRecent(workspace: string, identifier: string) {
  const list = readRecent(workspace)
  const left = list.filter((r) => r.identifier.toUpperCase() !== identifier.toUpperCase())
  if (left.length !== list.length) writeRecent(workspace, left)
}
