import type { Doc } from '@taskwell/shared/rich-text'
import type { PersonRef, Template } from '../../api.ts'

const UNITS = { day: 'day', week: 'week', month: 'month' } as const

// "Every 2 weeks · next Nov 10", "Every day · next Nov 10" or "Paused"; null for a template that doesn't repeat.
export function repeatSummary(t: Pick<Template, 'repeat'>, formatDate: (d: string) => string) {
  const r = t.repeat
  if (!r) return null
  if (r.paused) return 'Paused'
  const every = r.every === 1 ? `Every ${UNITS[r.unit]}` : `Every ${r.every} ${UNITS[r.unit]}s`
  return `${every} · next ${formatDate(r.next_on)}`
}

export type Fields = { title: string; description: Doc | null; statusId: string; priority: string; assignee: PersonRef | null; labelIds: string[] }

// Fills the new issue's empty fields from the template and keeps what the user already filled in. Status counts as
// empty until the user picks one (it starts on the first status); priority while it's "No priority".
// ponytail: emptiness is by value, so switching templates keeps the first one's fills; track per-field origin if
// people switch templates often.
export function fillEmpty(t: Template, f: Fields, statusPicked: boolean): Fields {
  return {
    title: f.title.trim() ? f.title : t.title || f.title,
    description: f.description ?? t.description,
    statusId: !statusPicked && t.status ? t.status.id : f.statusId,
    priority: f.priority === '0' && t.priority ? String(t.priority) : f.priority,
    assignee: f.assignee ?? t.assignee,
    labelIds: f.labelIds.length ? f.labelIds : t.labels.map((l) => l.id),
  }
}

// Templates for `project` first, then the rest; each group keeps its (by name) order.
export const forProjectFirst = (templates: Template[], project?: string) => {
  const mine = (t: Template) => Number(project !== undefined && t.project?.key === project)
  return [...templates].sort((a, b) => mine(b) - mine(a))
}
