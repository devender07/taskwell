import { extractDataFromArray } from '@taskwell/shared/list'
import { Checkbox } from '@taskwell/shared/checkbox'
import { StatusIcon, XIcon } from '@taskwell/shared/icons'
import { Popover } from '@taskwell/shared/popover'
import { SmartAutocomplete, type Option } from '@taskwell/shared/smart-autocomplete'
import type { Label, Member, PersonRef, Status, StatusCategory } from '@/api'
import type { PeopleSearch } from '@/components/workspace/member-pages'
import './issues.css'

export { StatusIcon }

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

export const ESTIMATES = [1, 2, 3, 5, 8] as const
export const points = (n: number) => `${n} ${n === 1 ? 'point' : 'points'}`
export const estimateOptions: Option[] = [{ value: '', label: 'No estimate' }, ...ESTIMATES.map((n) => ({ value: String(n), label: points(n) }))]
export const estimateOption = (e: number | null) => estimateOptions.find((o) => o.value === (e ? String(e) : '')) ?? null

export function EstimateChip({ estimate }: { estimate: number | null }) {
  if (!estimate) return null
  return (
    <span role="img" aria-label={`Estimate: ${points(estimate)}`} title={points(estimate)} className="inline-flex items-center justify-center flex-noshrink text-caption text-secondary rounded-full px-2" style={{ minWidth: 20, border: '1px solid rgba(var(--ink), 0.08)' }}>
      {estimate}
    </span>
  )
}

export const priorityOptions: Option[] = PRIORITIES.map((p) => ({ value: String(p.value), label: p.label }))
export const statusOptions = (statuses: Status[]): Option[] => statuses.map((s) => ({ value: s.id, label: s.name, icon: <StatusIcon status={s} /> }))
export const assigneeOptions = (members: Member[]): Option[] => [
  { value: '', label: 'No assignee' },
  ...members.map((m) => ({ value: m.id, label: m.name, hint: m.email })),
]
// For an assignee picker's loadOptions: the people matching what's typed, after "No assignee".
export const loadAssignees = (people: PeopleSearch) => async (q: string, signal: AbortSignal) => assigneeOptions(await people(q, signal))
export const assigneeOption = (a: PersonRef | null): Option | null => (a ? { value: a.id, label: a.name } : null)

export const AssignedBy = ({ issue }: { issue: { assignee: PersonRef | null; assigned_by: PersonRef | null } }) =>
  issue.assignee && issue.assigned_by ? <span className="text-caption text-secondary ellipsis">Assigned by {issue.assigned_by.name}</span> : null
export const priorityLabel = (p: number) => extractDataFromArray(PRIORITIES.find((x) => x.value === p), 'label', 'No priority')
export const categoryLabel = (c: StatusCategory) => extractDataFromArray(CATEGORIES.find((x) => x.value === c), 'label', c)

const today = () => new Date().toLocaleDateString('en-CA')

export const overdue = (due: string | null, category: StatusCategory) =>
  due !== null && due < today() && category !== 'completed' && category !== 'canceled'

export function formatDate(value: string) {
  const d = new Date(value.length === 10 ? `${value}T00:00` : value)
  const thisYear = d.getFullYear() === new Date().getFullYear()
  return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: thisYear ? undefined : 'numeric' })
}

export const formatRange = (c: { starts_on: string; ends_on: string }) => `${formatDate(c.starts_on)} – ${formatDate(c.ends_on)}`

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

// Picks labels by typing; the chosen ones show as removable chips. Remounted per pick so the field clears.
export function LabelSelect({ labels, value, onChange, error }: { labels: Label[]; value: string[]; onChange: (ids: string[]) => void; error?: string | null }) {
  const chosen = labels.filter((l) => value.includes(l.id))
  const options = labels.filter((l) => !value.includes(l.id)).map((l) => ({ value: l.id, label: l.name, icon: <LabelDot color={l.color} /> }))
  return (
    <div className="grid gap-2">
      <SmartAutocomplete
        key={value.length}
        label="Labels"
        placeholder={labels.length ? 'Add labels' : 'No labels yet'}
        disabled={options.length === 0}
        options={options}
        value={null}
        onChange={(o) => o && onChange([...value, o.value])}
        error={error}
      />
      {chosen.length ? (
        <span className="flex flex-wrap gap-2">
          {chosen.map((l) => (
            <span key={l.id} className="label-chip">
              <LabelDot color={l.color} />
              {l.name}
              <button type="button" className="label-chip-remove" aria-label={`Remove ${l.name}`} onClick={() => onChange(value.filter((id) => id !== l.id))}>
                <XIcon size={12} />
              </button>
            </span>
          ))}
        </span>
      ) : null}
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
