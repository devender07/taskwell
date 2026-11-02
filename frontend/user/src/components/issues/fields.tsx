import { Checkbox } from '@tether/shared/checkbox'
import { StatusTypeIcon } from '@tether/shared/icons'
import { Popover } from '@tether/shared/popover'
import type { Option } from '@tether/shared/smart-autocomplete'
import type { Label, Member, Status, StatusCategory } from '@/api'
import './issues.css'

export const PRIORITIES = [
  { value: 0, label: 'No priority' },
  { value: 1, label: 'Urgent' },
  { value: 2, label: 'High' },
  { value: 3, label: 'Medium' },
  { value: 4, label: 'Low' },
] as const

export const CATEGORIES: { value: StatusCategory; label: string }[] = [
  { value: 'backlog', label: 'Backlog' },
  { value: 'unstarted', label: 'Unstarted' },
  { value: 'started', label: 'Started' },
  { value: 'completed', label: 'Completed' },
  { value: 'canceled', label: 'Canceled' },
]

export const priorityOptions: Option[] = PRIORITIES.map((p) => ({ value: String(p.value), label: p.label }))
export const categoryOptions: Option[] = CATEGORIES
export const statusOptions = (statuses: Status[]): Option[] => statuses.map((s) => ({ value: s.id, label: s.name, icon: <StatusIcon status={s} /> }))
export const assigneeOptions = (members: Member[]): Option[] => [
  { value: '', label: 'No assignee' },
  ...members.map((m) => ({ value: m.id, label: m.name, hint: m.email })),
]

export const priorityLabel = (p: number) => PRIORITIES.find((x) => x.value === p)?.label ?? 'No priority'
export const categoryLabel = (c: StatusCategory) => CATEGORIES.find((x) => x.value === c)?.label ?? c

const today = () => new Date().toLocaleDateString('en-CA')

export const overdue = (due: string | null, category: StatusCategory) =>
  due !== null && due < today() && category !== 'completed' && category !== 'canceled'

export function formatDate(value: string) {
  const d = new Date(value.length === 10 ? `${value}T00:00` : value)
  const thisYear = d.getFullYear() === new Date().getFullYear()
  return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: thisYear ? undefined : 'numeric' })
}

export const formatRange = (c: { starts_on: string; ends_on: string }) => `${formatDate(c.starts_on)} – ${formatDate(c.ends_on)}`

type StatusLook = { category: StatusCategory; icon?: string | null; color?: string | null }

// A status's custom icon in its color when it has one, else its category's icon. Old snapshots without icon fall back.
export function StatusIcon({ status, label }: { status: StatusLook; label?: string }) {
  const { category, icon } = status
  const a11y = label ? { role: 'img', 'aria-label': label, 'aria-hidden': undefined } : { 'aria-hidden': true }
  const color = category === 'started' ? 'var(--text)' : category === 'completed' ? 'var(--success)' : 'var(--text-2)'
  if (icon) return <StatusTypeIcon icon={icon} color={status.color ?? color} title={label} {...a11y} />
  return (
    <svg width={14} height={14} viewBox="0 0 14 14" fill="none" stroke={color} strokeWidth={1.5} className="flex-noshrink" {...a11y}>
      {label ? <title>{label}</title> : null}
      {category === 'backlog' ? <circle cx={7} cy={7} r={6} strokeDasharray="2 2" /> : null}
      {category === 'unstarted' ? <circle cx={7} cy={7} r={6} /> : null}
      {category === 'started' ? (
        <>
          <circle cx={7} cy={7} r={6} />
          <path d="M7 3a4 4 0 0 1 0 8z" fill={color} stroke="none" />
        </>
      ) : null}
      {category === 'completed' || category === 'canceled' ? (
        <>
          <circle cx={7} cy={7} r={6.5} fill={color} stroke="none" />
          <path d={category === 'completed' ? 'm4.5 7 1.75 1.75L9.5 5.5' : 'm5 5 4 4M9 5 5 9'} stroke="var(--bg)" strokeLinecap="round" strokeLinejoin="round" />
        </>
      ) : null}
    </svg>
  )
}

export function LabelDot({ color }: { color: string }) {
  return <span aria-hidden className="label-dot" style={{ background: color }} />
}

// Every workspace label as a checkbox; `value` is the checked label ids.
export function LabelChecklist({ labels, value, onChange, disabled }: { labels: Label[]; value: string[]; onChange: (ids: string[]) => void; disabled?: boolean }) {
  if (labels.length === 0) return <p className="text-caption text-secondary">No labels yet.</p>
  return (
    <div className="grid gap-2">
      {labels.map((l) => (
        <Checkbox
          key={l.id}
          disabled={disabled}
          checked={value.includes(l.id)}
          onChange={(e) => onChange(e.target.checked ? [...value, l.id] : value.filter((id) => id !== l.id))}
          label={
            <span className="flex items-center gap-2" style={{ minWidth: 0 }}>
              <LabelDot color={l.color} />
              <span className="ellipsis">{l.name}</span>
            </span>
          }
        />
      ))}
    </div>
  )
}

// The issue's labels as chips; the button opens a checklist that saves on every change.
export function LabelsPicker({ labels, value, onChange }: { labels: Label[]; value: string[]; onChange: (ids: string[]) => void }) {
  const chosen = labels.filter((l) => value.includes(l.id))
  return (
    <Popover
      aria-label="Labels"
      trigger={(props) => (
        <button type="button" className="labels-trigger" {...props}>
          {chosen.length === 0 ? <span className="text-secondary">Add labels</span> : <LabelChips labels={chosen} />}
        </button>
      )}
    >
      <div className="p-3" style={{ maxHeight: 320, overflowY: 'auto' }}>
        <LabelChecklist labels={labels} value={value} onChange={onChange} />
      </div>
    </Popover>
  )
}

export function LabelChips({ labels }: { labels: Label[] }) {
  return (
    <span className="flex flex-wrap gap-2">
      {labels.map((l) => (
        <span key={l.id} className="label-chip">
          <LabelDot color={l.color} />
          {l.name}
        </span>
      ))}
    </span>
  )
}
