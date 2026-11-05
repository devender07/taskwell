import { Link, useRouteContext, useRouter } from '@tanstack/react-router'
import { startTransition, useActionState, useMemo, useOptimistic, useRef, type ReactNode } from 'react'
import { Avatar } from '@taskwell/shared/avatar'
import { DatePicker } from '@taskwell/shared/date-picker'
import { filterOptions } from '@taskwell/shared/match'
import { SmartAutocomplete, type Option } from '@taskwell/shared/smart-autocomplete'
import { api, canViewMember, cyclesPath, issuePath, avatarSrc, listCycles, toFormErrors, type CycleRef, type Issue, type IssueRef, type Label, type Status, type Workspace } from '@/api'
import type { PeopleSearch } from '@/components/workspace/member-pages'
import { reloadPage, subscribePaths } from '@/realtime'
import { FormError } from '@/components/auth/auth-card'
import { AssignedBy, LabelChips, LabelsPicker, StatusIcon, assigneeOption, loadAssignees, formatDate, formatRange, overdue, priorityLabel, priorityOptions, statusOptions } from './fields'
import { refOption, searchIssues, subscribeIssues } from './relations'
import '@/components/analytics/analytics.css'
import { byId, extractDataFromArray } from '@taskwell/shared/list'

type Props = { workspace: Workspace; issue: Issue; statuses: Status[]; labels: Label[]; people: PeopleSearch }
type Patch = { form: FormData; next: Partial<Issue> }

// Each property saves on change with a partial update; read-only text without edit rights.
export function IssueProperties({ workspace, issue: saved, statuses, labels, people }: Props) {
  const router = useRouter()
  const { user } = useRouteContext({ from: '/_app' })
  const [issue, apply] = useOptimistic(saved, (cur, next: Partial<Issue>) => ({ ...cur, ...next }))
  const statusOpts = useMemo(() => statusOptions(statuses), [statuses])
  const found = useRef(new Map<string, IssueRef>())
  const foundCycles = useRef(new Map<string, CycleRef>())

  const [error, save] = useActionState(async (_prev: string | null, { form, next }: Patch) => {
    apply(next)
    try {
      await api<Issue>(issuePath(workspace, saved.identifier), form)
    } catch (e) {
      const { errors, error } = toFormErrors(e)
      return Object.values(errors).find(Boolean) ?? error
    }
    await reloadPage(router)
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
  const parentable = !extractDataFromArray(issue, 'sub_issues', []).length

  async function loadParents(q: string, signal: AbortSignal) {
    const refs = await searchIssues(workspace, saved.identifier, saved.project.key)(q, signal)
    for (const r of refs) found.current.set(r.identifier, r)
    return [...(q ? [] : [{ value: '', label: 'No parent' }]), ...refs.map(refOption)]
  }

  // This project's upcoming and current cycles, plus the issue's own even when it's past.
  async function loadCycles(q: string, signal: AbortSignal) {
    const all = await listCycles(workspace.slug, saved.project.key, signal)
    const shown = all.filter((c) => c.state !== 'past' || c.id === issue.cycle?.id)
    for (const c of shown) foundCycles.current.set(c.id, c)
    return filterOptions([{ value: '', label: 'No cycle' }, ...shown.map(cycleOption)], q)
  }

  return (
    <section className="grid gap-4 p-6 bg-surface border rounded-lg" aria-label="Properties">
      {edit ? (
        <>
          <SmartAutocomplete
            label="Status"
            options={statusOpts}
            value={statusOpts.find((o) => o.value === issue.status.id) ?? null}
            onChange={(o) => {
              const status = byId(statuses, o?.value)
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
            loadOptions={loadAssignees(people)}
            value={assigneeOption(issue.assignee)}
            onChange={(o) => {
              const id = extractDataFromArray(o, 'value', '')
              if (id === (extractDataFromArray(issue.assignee, 'id', ''))) return
              update('assignee_id', [id], { assignee: o && id ? { id, name: o.label } : null, assigned_by: { id: user.id, name: user.name } })
            }}
          />
          <AssignedBy issue={issue} />
          <DatePicker
            label="Due date"
            placeholder="No due date"
            value={issue.due_date ?? ''}
            onChange={(v) => v !== (issue.due_date ?? '') && update('due_date', [v], { due_date: v || null })}
          />
          {parentable ? (
            <SmartAutocomplete
              label="Parent"
              placeholder="No parent"
              loadOptions={loadParents}
              subscribe={subscribeIssues(workspace)}
              value={issue.parent ? refOption(issue.parent) : null}
              onChange={(o) => {
                const id = extractDataFromArray(o, 'value', '')
                if (id === (extractDataFromArray(issue.parent, 'identifier', ''))) return
                update('parent', [id], { parent: found.current.get(id) ?? null })
              }}
            />
          ) : null}
          <SmartAutocomplete
            label="Cycle"
            placeholder="No cycle"
            loadOptions={loadCycles}
            subscribe={(reload) => subscribePaths([cyclesPath(workspace.slug, saved.project.key)], reload)}
            value={issue.cycle ? cycleOption(issue.cycle) : null}
            onChange={(o) => {
              const id = extractDataFromArray(o, 'value', '')
              if (id === (extractDataFromArray(issue.cycle, 'id', ''))) return
              update('cycle', [id], { cycle: foundCycles.current.get(id) ?? null })
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
          {issue.can_change_status ? (
            <>
              <SmartAutocomplete
                label="Status"
                options={statusOpts}
                value={statusOpts.find((o) => o.value === issue.status.id) ?? null}
                onChange={(o) => {
                  const status = byId(statuses, o?.value)
                  if (status && status.id !== issue.status.id) update('status_id', [status.id], { status })
                }}
              />
              <FormError>{error}</FormError>
            </>
          ) : (
            <Property label="Status">
              <span className="flex items-center gap-2">
                <StatusIcon status={issue.status} />
                {issue.status.name}
              </span>
            </Property>
          )}
          <Property label="Priority">{priorityLabel(issue.priority)}</Property>
          <Property label="Assignee">
            {issue.assignee ? (
              <span className="flex items-center gap-2">
                <Avatar name={issue.assignee.name} size={24} src={avatarSrc(issue.assignee.id)} />
                {canViewMember(workspace, issue.assignee.id, user.id) ? (
                  <Link to="/$workspace/members/$id" params={{ workspace: workspace.slug, id: issue.assignee.id }} className="member-link">
                    {issue.assignee.name}
                  </Link>
                ) : (
                  issue.assignee.name
                )}
              </span>
            ) : (
              <span className="text-secondary">No assignee</span>
            )}
            <AssignedBy issue={issue} />
          </Property>
          <Property label="Due date">
            {issue.due_date ? <span className={late ? 'text-accent' : undefined}>{formatDate(issue.due_date)}</span> : <span className="text-secondary">No due date</span>}
          </Property>
          {parentable ? (
            <Property label="Parent">
              {issue.parent ? (
                <Link to="/$workspace/issue/$identifier" params={{ workspace: workspace.slug, identifier: issue.parent.identifier }}>
                  {issue.parent.identifier} {issue.parent.title}
                </Link>
              ) : (
                <span className="text-secondary">No parent</span>
              )}
            </Property>
          ) : null}
          <Property label="Cycle">
            {issue.cycle ? (
              <Link
                to="/$workspace/projects/$key/cycles/$number"
                params={{ workspace: workspace.slug, key: issue.project.key, number: String(issue.cycle.number) }}
              >
                {issue.cycle.name}
              </Link>
            ) : (
              <span className="text-secondary">No cycle</span>
            )}
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

const cycleOption = (c: CycleRef): Option => ({ value: c.id, label: c.name, hint: formatRange(c) })

function Property({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid gap-1">
      <span className="text-label text-secondary">{label}</span>
      <span className="text-body-sm">{children}</span>
    </div>
  )
}
