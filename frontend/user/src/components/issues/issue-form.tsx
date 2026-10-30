import { useNavigate, useRouter } from '@tanstack/react-router'
import { useActionState, useMemo, useState } from 'react'
import { Button } from '@tether/shared/button'
import { DatePicker } from '@tether/shared/date-picker'
import { Dialog } from '@tether/shared/dialog'
import { Input } from '@tether/shared/input'
import { RichEditor, type Doc } from '@tether/shared/rich-editor'
import { SmartAutocomplete } from '@tether/shared/smart-autocomplete'
import { dueDate, issueDescription, issueTitle } from '@tether/shared/validate'
import { api, toFormErrors, type FieldErrors, type Issue, type IssueRef, type Label, type Member, type Project, type Status, type Workspace } from '@/api'
import { FormError } from '@/components/auth/auth-card'
import { LabelChecklist, assigneeOptions, priorityOptions, statusOptions } from './fields'

type State = { errors?: FieldErrors; error?: string | null }

export const issuePath = (workspace: Workspace, identifier: string) =>
  `/workspaces/${encodeURIComponent(workspace.slug)}/issues/${encodeURIComponent(identifier)}`

type CreateProps = { workspace: Workspace; project: Project; parent?: IssueRef; statuses: Status[]; labels: Label[]; members: Member[] }

// Labels are checkboxes here, not the labels popover: a popover can't render above a modal dialog.
export function CreateIssueForm({ workspace, project, parent, statuses, labels, members }: CreateProps) {
  const navigate = useNavigate()
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState<{ doc: Doc | null; length: number }>({ doc: null, length: 0 })
  const [statusId, setStatusId] = useState(statuses[0]?.id ?? '')
  const [priority, setPriority] = useState('0')
  const [assigneeId, setAssigneeId] = useState('')
  const [due, setDue] = useState('')
  const [labelIds, setLabelIds] = useState<string[]>([])
  const statusOpts = useMemo(() => statusOptions(statuses), [statuses])
  const assigneeOpts = useMemo(() => assigneeOptions(members), [members])
  const mentions = useMemo(() => members.map((m) => ({ id: m.id, name: m.name })), [members])

  const [{ errors, error }, submit, pending] = useActionState(async (): Promise<State> => {
    const errors = { title: issueTitle(title), description: issueDescription(description.length), due_date: dueDate(due) }
    if (Object.values(errors).some(Boolean)) return { errors }
    const form = new FormData()
    form.set('title', title.trim())
    form.set('description', description.doc ? JSON.stringify(description.doc) : '')
    if (statusId) form.set('status_id', statusId)
    if (priority) form.set('priority', priority)
    form.set('assignee_id', assigneeId)
    form.set('due_date', due)
    for (const id of labelIds) form.append('label_ids', id)
    if (parent) form.set('parent', parent.identifier)
    let issue: Issue
    try {
      issue = await api<Issue>(`/workspaces/${encodeURIComponent(workspace.slug)}/projects/${encodeURIComponent(project.key)}/issues`, form)
    } catch (e) {
      return toFormErrors(e)
    }
    await navigate({ to: '/$workspace/issue/$identifier', params: { workspace: workspace.slug, identifier: issue.identifier } })
    return {}
  }, {})

  return (
    <form action={submit} noValidate className="grid gap-4">
      {parent ? (
        <div className="grid gap-1">
          <p className="text-body-sm text-secondary m-0">Sub-issue of {parent.identifier}</p>
          <FormError>{errors?.parent}</FormError>
        </div>
      ) : null}
      <Input label="Title" placeholder="Issue title" autoComplete="off" autoFocus value={title} onChange={(e) => setTitle(e.target.value)} error={errors?.title} />
      <RichEditor
        label="Description"
        placeholder="Add a description… Type @ to mention someone"
        mentions={mentions}
        onChange={(doc, length) => setDescription({ doc, length })}
        error={errors?.description}
      />
      <div className="grid gap-4" style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(160px, 1fr))' }}>
        <SmartAutocomplete
          label="Status"
          options={statusOpts}
          value={statusOpts.find((o) => o.value === statusId) ?? null}
          onChange={(o) => setStatusId(o?.value ?? '')}
          error={errors?.status_id}
        />
        <SmartAutocomplete
          label="Priority"
          options={priorityOptions}
          value={priorityOptions.find((o) => o.value === priority) ?? null}
          onChange={(o) => setPriority(o?.value ?? '')}
          error={errors?.priority}
        />
        <SmartAutocomplete
          label="Assignee"
          placeholder="No assignee"
          options={assigneeOpts}
          value={assigneeOpts.find((o) => o.value === assigneeId && o.value) ?? null}
          onChange={(o) => setAssigneeId(o?.value ?? '')}
          error={errors?.assignee_id}
        />
        <DatePicker label="Due date" placeholder="No due date" value={due} onChange={setDue} error={errors?.due_date} />
      </div>
      <fieldset className="grid gap-3 m-0 p-0" style={{ border: 0 }}>
        <legend className="text-label text-secondary mb-2 p-0">Labels</legend>
        <LabelChecklist labels={labels} value={labelIds} onChange={setLabelIds} />
        <FormError>{errors?.label_ids}</FormError>
      </fieldset>
      <FormError>{error}</FormError>
      <Button type="submit" className="w-full" disabled={pending}>
        {pending ? 'Creating issue…' : 'Create issue'}
      </Button>
    </form>
  )
}

export function EditIssueForm({ workspace, issue, onDone }: { workspace: Workspace; issue: Issue; onDone: () => void }) {
  const router = useRouter()
  const [title, setTitle] = useState(issue.title)

  const [{ errors, error }, submit, pending] = useActionState(async (): Promise<State> => {
    const errors = { title: issueTitle(title) }
    if (errors.title) return { errors }
    const form = new FormData()
    form.set('title', title.trim())
    try {
      await api<Issue>(issuePath(workspace, issue.identifier), form)
    } catch (e) {
      return toFormErrors(e)
    }
    await router.invalidate()
    onDone()
    return {}
  }, {})

  return (
    <form action={submit} noValidate className="grid gap-4">
      <Input label="Title" autoComplete="off" autoFocus value={title} onChange={(e) => setTitle(e.target.value)} error={errors?.title} />
      <FormError>{error}</FormError>
      <div className="flex items-center gap-3">
        <Button type="submit" disabled={pending}>
          {pending ? 'Saving…' : 'Save changes'}
        </Button>
        <Button variant="ghost" onClick={() => onDone()} disabled={pending}>
          Cancel
        </Button>
      </div>
    </form>
  )
}

export function DeleteIssue({ workspace, issue }: { workspace: Workspace; issue: Issue }) {
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)

  const [error, remove, pending] = useActionState(async () => {
    try {
      await api(issuePath(workspace, issue.identifier), undefined, 'DELETE')
    } catch (e) {
      return toFormErrors(e).error
    }
    await navigate({ to: '/$workspace/projects/$key', params: { workspace: workspace.slug, key: issue.project.key }, replace: true })
    return null
  }, null)

  return (
    <>
      <Button variant="ghost" style={{ justifySelf: 'start' }} onClick={() => setOpen(true)}>
        Delete issue
      </Button>
      <Dialog open={open} onClose={() => setOpen(false)} title={`Delete ${issue.identifier}?`} description="This can't be undone.">
        <form action={remove} className="grid gap-4">
          <FormError>{error}</FormError>
          <Button type="submit" variant="danger" className="w-full" disabled={pending}>
            {pending ? 'Deleting…' : 'Delete issue'}
          </Button>
        </form>
      </Dialog>
    </>
  )
}
