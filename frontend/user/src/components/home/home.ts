import type { Issue } from '@/api'
import { DAY } from '@taskwell/shared/time'

export type Recent = { identifier: string; title: string }

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

// Kept per workspace URL, with the workspace's id: one deleted and made again at the same URL starts empty.
export const recentKey = (slug: string) => `taskwell:recent:${slug}`
type Stored = { workspace: string; items: Recent[] }

function readStored(slug: string): Stored | null {
  try {
    const v = JSON.parse(localStorage.getItem(recentKey(slug)) ?? 'null')
    if (typeof v?.workspace !== 'string' || !Array.isArray(v.items)) return null
    return { workspace: v.workspace, items: v.items.filter((r: Recent) => typeof r?.identifier === 'string' && typeof r?.title === 'string') }
  } catch {
    return null
  }
}

function writeStored(slug: string, stored: Stored) {
  try {
    localStorage.setItem(recentKey(slug), JSON.stringify(stored))
  } catch {}
}

export function readRecent(slug: string, workspaceId: string): Recent[] {
  const stored = readStored(slug)
  return stored?.workspace === workspaceId ? stored.items : []
}

export const recordRecent = (slug: string, workspaceId: string, item: Recent) =>
  writeStored(slug, { workspace: workspaceId, items: pushRecent(readRecent(slug, workspaceId), item) })

export function pruneRecent(slug: string, identifier: string) {
  const stored = readStored(slug)
  if (!stored) return
  const items = stored.items.filter((r) => r.identifier.toUpperCase() !== identifier.toUpperCase())
  if (items.length !== stored.items.length) writeStored(slug, { ...stored, items })
}
