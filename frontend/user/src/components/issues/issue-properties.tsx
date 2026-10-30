import { useRouter } from '@tanstack/react-router'
import { startTransition, useActionState, useMemo, useOptimistic, type ReactNode } from 'react'
import { Avatar } from '@tether/shared/avatar'
import { Input } from '@tether/shared/input'
import { SmartAutocomplete } from '@tether/shared/smart-autocomplete'
import { dueDate } from '@tether/shared/validate'
import { api, toFormErrors, type Issue, type Label, type Member, type Status, type Workspace } from '@/api'
import { FormError } from '@/components/auth/auth-card'
import { LabelChips, LabelsPicker, StatusIcon, assigneeOptions, formatDate, overdue, priorityLabel, priorityOptions, statusOptions } from './fields'
import { issuePath } from './issue-form'

type Props = { workspace: Workspace; issue: Issue; statuses: Status[]; labels: Label[]; members: Member[] }
type Patch = { form: FormData; next: Partial<Issue> }

// Each property saves on change with a partial update; read-only text without edit rights.
export function IssueProperties({ workspace, issue: saved, statuses, labels, members }: Props) {
  const router = useRouter()
  const [issue, apply] = useOptimistic(saved, (cur, next: Partial<Issue>) => ({ ...cur, ...next }))
  const statusOpts = useMemo(() => statusOptions(statuses), [statuses])
  const assigneeOpts = useMemo(() => assigneeOptions(members), [members])

  const [error, save] = useActionState(async (_prev: string | null, { form, next }: Patch) => {
    apply(next)
    try {
      await api<Issue>(issuePath(workspace, saved.identifier), form)
    } catch (e) {
      const { errors, error } = toFormErrors(e)
      return Object.values(errors).find(Boolean) ?? error
    }
    await router.invalidate()
    return null
  }, null)

  function update(field: string, values: string[], next: Partial<Issue>) {
    const form = new FormData()
    if (values.length === 0) form.set(field, '')
    for (const v of values) form.append(field, v)
    startTransition(() => save({ form, next }))
  }

  const late = overdue(issue.due_date, issue.status.category)
  const edit = issue.can_edit

  return (
    <section className="grid gap-4 p-6 bg-surface border rounded-lg" aria-label="Properties">
      {edit ? (
        <>
          <SmartAutocomplete
            label="Status"
            options={statusOpts}
            value={statusOpts.find((o) => o.value === issue.status.id) ?? null}
            onChange={(o) => {
              const status = statuses.find((s) => s.id === o?.value)
              if (status && status.id !== issue.status.id) update('status_id', [status.id], { status })
            }}
          />
          <SmartAutocomplete
            label="Priority"
            options={priorityOptions}
            value={priorityOptions.find((o) => o.value === String(issue.priority)) ?? null}
            onChange={(o) => o && o.value !== String(issue.priority) && update('priority', [o.value], { priority: Number(o.value) })}
          />
          <SmartAutocomplete
            label="Assignee"
            placeholder="No assignee"
            options={assigneeOpts}
            value={assigneeOpts.find((o) => o.value && o.value === issue.assignee?.id) ?? null}
            onChange={(o) => {
              const id = o?.value ?? ''
              if (id === (issue.assignee?.id ?? '')) return
              const m = members.find((m) => m.id === id)
              update('assignee_id', [id], { assignee: m ? { id: m.id, name: m.name } : null })
            }}
          />
          <Input
            key={issue.due_date ?? ''}
            label="Due date"
            type="date"
            defaultValue={issue.due_date ?? ''}
            onKeyDown={(e) => e.key === 'Enter' && e.currentTarget.blur()}
            onBlur={(e) => {
              const v = e.target.value
              if (v !== (issue.due_date ?? '') && !dueDate(v)) update('due_date', [v], { due_date: v || null })
            }}
          />
          <div className="grid gap-2">
            <span className="text-label text-secondary">Labels</span>
            <LabelsPicker
              labels={labels}
              value={issue.labels.map((l) => l.id)}
              onChange={(ids) => update('label_ids', ids, { labels: labels.filter((l) => ids.includes(l.id)) })}
            />
          </div>
          <FormError>{error}</FormError>
        </>
      ) : (
        <>
          <Property label="Status">
            <span className="flex items-center gap-2">
              <StatusIcon category={issue.status.category} />
              {issue.status.name}
            </span>
          </Property>
          <Property label="Priority">{priorityLabel(issue.priority)}</Property>
          <Property label="Assignee">
            {issue.assignee ? (
              <span className="flex items-center gap-2">
                <Avatar name={issue.assignee.name} size={24} />
                {issue.assignee.name}
              </span>
            ) : (
              <span className="text-secondary">No assignee</span>
            )}
          </Property>
          <Property label="Due date">
            {issue.due_date ? <span className={late ? 'text-accent' : undefined}>{formatDate(issue.due_date)}</span> : <span className="text-secondary">No due date</span>}
          </Property>
          <Property label="Labels">
            {issue.labels.length ? <LabelChips labels={issue.labels} /> : <span className="text-secondary">No labels</span>}
          </Property>
        </>
      )}
      <div className="grid gap-1 pt-4" style={{ borderTop: '1px solid var(--border)' }}>
        <span className="text-caption text-secondary">
          Created{issue.created_by ? ` by ${issue.created_by.name}` : ''} on {formatDate(issue.created_at)}
        </span>
        <span className="text-caption text-secondary">Updated {formatDate(issue.updated_at)}</span>
      </div>
    </section>
  )
}

function Property({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid gap-1">
      <span className="text-label text-secondary">{label}</span>
      <span className="text-body-sm">{children}</span>
    </div>
  )
}
