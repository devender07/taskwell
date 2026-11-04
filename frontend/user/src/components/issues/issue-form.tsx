import { useNavigate, useRouter } from '@tanstack/react-router'
import { useActionState, useMemo, useState, type ReactNode } from 'react'
import { Button } from '@taskwell/shared/button'
import { DatePicker } from '@taskwell/shared/date-picker'
import { Dialog } from '@taskwell/shared/dialog'
import { Input } from '@taskwell/shared/input'
import type { Doc } from '@taskwell/shared/rich-text'
import { RichEditor } from './rich-editor'
import { SmartAutocomplete, type Option } from '@taskwell/shared/smart-autocomplete'
import { dueDate, issueDescription, issueTitle } from '@taskwell/shared/validate'
import { api, feedbackPath, issuePath, toFormErrors, updateFeedback, uploadFile, type CycleRef, type FieldErrors, type Issue, type IssueRef, type Label, type Project, type Status, type Workspace, workspacePath } from '@/api'
import { refetchPaths, reloadPage } from '@/realtime'
import { FormError } from '@/components/auth/auth-card'
import type { PeopleSearch } from '@/components/workspace/member-pages'
import { LabelSelect, loadAssignees, priorityOptions, statusOptions } from './fields'

type State = { errors?: FieldErrors; error?: string | null }

// Quick-create filled in from elsewhere (a failed test's "Create bug", a feedback's "Create issue"); `blocks` links the
// new issue as blocking that one, `feedback` links that feedback to the new issue.
export type Prefill = { title: string; description: Doc; blocks?: string; feedback?: string }

type CreateProps = { workspace: Workspace; project: Project; projectPicker?: ReactNode; parent?: IssueRef; cycle?: CycleRef; prefill?: Prefill; statuses: Status[]; labels: Label[]; people: PeopleSearch }

export function CreateIssueForm({ workspace, project, projectPicker, parent, cycle, prefill, statuses, labels, people }: CreateProps) {
  const navigate = useNavigate()
  const [title, setTitle] = useState(prefill?.title ?? '')
  const [description, setDescription] = useState<{ doc: Doc | null; length: number }>({ doc: prefill?.description ?? null, length: 0 })
  const [statusId, setStatusId] = useState(statuses[0]?.id ?? '')
  const [priority, setPriority] = useState('0')
  const [assignee, setAssignee] = useState<Option | null>(null)
  const [due, setDue] = useState('')
  const [labelIds, setLabelIds] = useState<string[]>([])
  const statusOpts = useMemo(() => statusOptions(statuses), [statuses])

  const [{ errors, error }, submit, pending] = useActionState(async (): Promise<State> => {
    const errors = { title: issueTitle(title), description: issueDescription(description.length), due_date: dueDate(due) }
    if (Object.values(errors).some(Boolean)) return { errors }
    const form = new FormData()
    form.set('title', title.trim())
    form.set('description', description.doc ? JSON.stringify(description.doc) : '')
    if (statusId) form.set('status_id', statusId)
    if (priority) form.set('priority', priority)
    form.set('assignee_id', assignee?.value ?? '')
    form.set('due_date', due)
    for (const id of labelIds) form.append('label_ids', id)
    if (parent) form.set('parent', parent.identifier)
    if (cycle) form.set('cycle', cycle.id)
    let issue: Issue
    try {
      issue = await api<Issue>(`${workspacePath(workspace.slug)}/projects/${encodeURIComponent(project.key)}/issues`, form)
    } catch (e) {
      return toFormErrors(e)
    }
    if (prefill?.blocks) {
      const link = new FormData()
      link.set('type', 'blocks')
      link.set('issue', prefill.blocks)
      await api(`${issuePath(workspace, issue.identifier)}/links`, link).catch(() => {})
    }
    if (prefill?.feedback) {
      await updateFeedback(workspace.slug, prefill.feedback, { issue: issue.identifier }).catch(() => {})
      refetchPaths([feedbackPath(workspace.slug), `${feedbackPath(workspace.slug)}/count`])
    }
    await navigate({ to: '/$workspace/issue/$identifier', params: { workspace: workspace.slug, identifier: issue.identifier } })
    return {}
  }, {})

  return (
    <form action={submit} noValidate className="issue-form">
      <div className="issue-form-main">
        {parent ? (
          <div className="grid gap-1">
            <p className="text-body-sm text-secondary m-0">Sub-issue of {parent.identifier}</p>
            <FormError>{errors?.parent}</FormError>
          </div>
        ) : null}
        {cycle ? (
          <div className="grid gap-1">
            <p className="text-body-sm text-secondary m-0">In {cycle.name}</p>
            <FormError>{errors?.cycle}</FormError>
          </div>
        ) : null}
        <Input label="Title" placeholder="Issue title" autoComplete="off" autoFocus value={title} onChange={(e) => setTitle(e.target.value)} error={errors?.title} />
        <RichEditor
          label="Description"
          placeholder="Add a description… Type @ to mention someone"
          mentions={people}
          minHeight={280}
          content={prefill?.description}
          onChange={(doc, length) => setDescription({ doc, length })}
          error={errors?.description}
          upload={(file) => uploadFile(workspace.slug, file)}
        />
      </div>
      <div className="grid gap-4" style={{ alignContent: 'start' }}>
        {projectPicker}
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
          loadOptions={loadAssignees(people)}
          value={assignee}
          onChange={(o) => setAssignee(o?.value ? o : null)}
          error={errors?.assignee_id}
        />
        <DatePicker label="Due date" placeholder="No due date" value={due} onChange={setDue} error={errors?.due_date} />
        <LabelSelect labels={labels} value={labelIds} onChange={setLabelIds} error={errors?.label_ids} />
      </div>
      <div className="issue-form-footer">
        <FormError>{error}</FormError>
        <Button type="submit" disabled={pending}>
          {pending ? 'Creating issue…' : 'Create issue'}
        </Button>
      </div>
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
    await reloadPage(router)
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
      <Button variant="ghost" style={{ justifySelf: 'start', color: 'var(--accent)' }} onClick={() => setOpen(true)}>
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
