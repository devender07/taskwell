import { Link, useRouter } from '@tanstack/react-router'
import { startTransition, useActionState, useState, type ReactNode } from 'react'
import { Avatar } from '@taskwell/shared/avatar'
import { Button } from '@taskwell/shared/button'
import { Dialog } from '@taskwell/shared/dialog'
import { IconButton } from '@taskwell/shared/icon-button'
import { SmartAutocomplete, type Option } from '@taskwell/shared/smart-autocomplete'
import { XIcon } from '@taskwell/shared/icons'
import { api, issuePath, avatarSrc, toFormErrors, type FieldErrors, type Issue, type IssueRef, type Label, type Project, type Status, type Workspace, workspacePath } from '@/api'
import type { PeopleSearch } from '@/components/workspace/member-pages'
import { FormError } from '@/components/auth/auth-card'
import { reloadPage, subscribePaths } from '@/realtime'
import { StatusIcon } from './fields'
import { CreateIssueForm } from './issue-form'

export const refOption = (r: IssueRef): Option => ({ value: r.identifier, label: `${r.identifier} ${r.title}`, hint: r.status.name })

// Reloads an open issue picker when issues in the workspace change.
export const subscribeIssues = (workspace: Workspace) => (reload: () => void) => {
  const ws = `${workspacePath(workspace.slug)}`
  return subscribePaths([`${ws}/issues/`, `${ws}/projects/`], reload)
}

// Issue search for pickers, minus the issue being edited; `project` limits it to one project key.
export function searchIssues(workspace: Workspace, exclude: string, project?: string) {
  return async (q: string, signal: AbortSignal) => {
    const params = new URLSearchParams({ q })
    if (project) params.set('project', project)
    const refs = await api<IssueRef[]>(`${workspacePath(workspace.slug)}/issues?${params}`, undefined, 'GET', signal)
    return refs.filter((r) => r.identifier !== exclude)
  }
}

function RefRow({ workspace, issue, children }: { workspace: string; issue: IssueRef; children?: ReactNode }) {
  return (
    <li className="flex items-center" style={{ paddingRight: children ? 16 : 0 }}>
      <Link to="/$workspace/issue/$identifier" params={{ workspace, identifier: issue.identifier }} className="issue-row flex-grow" style={{ minWidth: 0 }}>
        <StatusIcon status={issue.status} label={issue.status.name} />
        <span className="issue-id flex-noshrink" style={{ minWidth: 64 }}>{issue.identifier}</span>
        <span className="text-label text-primary ellipsis flex-grow">{issue.title}</span>
        <span className="issue-row-wide text-caption text-secondary flex-noshrink">{issue.status.name}</span>
        {issue.assignee ? (
          <span role="img" aria-label={`Assigned to ${issue.assignee.name}`} title={issue.assignee.name} className="inline-grid flex-noshrink">
            <Avatar name={issue.assignee.name} size={24} src={avatarSrc(issue.assignee.id)} />
          </span>
        ) : null}
      </Link>
      {children}
    </li>
  )
}

type SubIssuesProps = { workspace: Workspace; issue: Issue; project: Project; create: boolean; statuses: Status[]; labels: Label[]; people: PeopleSearch }

export function SubIssues({ workspace, issue, project, create, statuses, labels, people }: SubIssuesProps) {
  const [creating, setCreating] = useState(false)
  const subs = issue.sub_issues ?? []

  return (
    <section className="grid gap-4" aria-labelledby="sub-issues-heading">
      <div className="flex items-center justify-between gap-4">
        <h2 id="sub-issues-heading" className="text-label">Sub-issues</h2>
        {create ? (
          <Button variant="secondary" onClick={() => setCreating(true)}>
            Add sub-issue
          </Button>
        ) : null}
      </div>
      {subs.length === 0 ? (
        <p className="text-body-sm text-secondary m-0">No sub-issues.</p>
      ) : (
        <ul className="grid m-0 p-0 bg-surface border rounded-lg overflow-hidden" style={{ listStyle: 'none' }}>
          {subs.map((s) => (
            <RefRow key={s.id} workspace={workspace.slug} issue={s} />
          ))}
        </ul>
      )}
      {create ? (
        <Dialog open={creating} onClose={() => setCreating(false)} title="New issue" wide>
          <CreateIssueForm workspace={workspace} project={project} parent={issue} statuses={statuses} labels={labels} people={people} />
        </Dialog>
      ) : null}
    </section>
  )
}

const linkTypes: Option[] = [
  { value: 'blocks', label: 'Blocks' },
  { value: 'blocked_by', label: 'Blocked by' },
]

export function Relations({ workspace, issue }: { workspace: Workspace; issue: Issue }) {
  const router = useRouter()
  const groups = [
    { label: 'Blocks', refs: issue.blocks ?? [] },
    { label: 'Blocked by', refs: issue.blocked_by ?? [] },
  ].filter((g) => g.refs.length > 0)

  const [removeError, remove, removing] = useActionState(async (_prev: string | null, other: string) => {
    try {
      await api<Issue>(`${issuePath(workspace, issue.identifier)}/links/${encodeURIComponent(other)}`, undefined, 'DELETE')
    } catch (e) {
      return toFormErrors(e).error
    }
    await reloadPage(router)
    return null
  }, null)

  return (
    <section className="grid gap-4" aria-labelledby="relations-heading">
      <h2 id="relations-heading" className="text-label">Relations</h2>
      {groups.length === 0 ? <p className="text-body-sm text-secondary m-0">No links.</p> : null}
      {groups.map((g) => (
        <div key={g.label} className="grid gap-2">
          <h3 className="text-caption text-secondary m-0">{g.label}</h3>
          <ul className="grid m-0 p-0 bg-surface border rounded-lg overflow-hidden" style={{ listStyle: 'none' }}>
            {g.refs.map((r) => (
              <RefRow key={r.id} workspace={workspace.slug} issue={r}>
                {issue.can_edit ? (
                  <IconButton aria-label={`Remove link to ${r.identifier}`} tooltip="Remove link" disabled={removing} onClick={() => startTransition(() => remove(r.identifier))}>
                    <XIcon size={20} strokeWidth={2} />
                  </IconButton>
                ) : null}
              </RefRow>
            ))}
          </ul>
        </div>
      ))}
      <FormError>{removeError}</FormError>
      {issue.can_edit ? <AddLink workspace={workspace} issue={issue} /> : null}
    </section>
  )
}

type State = { errors?: FieldErrors; error?: string | null }

function AddLink({ workspace, issue }: { workspace: Workspace; issue: Issue }) {
  const router = useRouter()
  const [type, setType] = useState<Option | null>(linkTypes[0] ?? null)
  const [target, setTarget] = useState<Option | null>(null)

  const [{ errors, error }, submit, pending] = useActionState(async (): Promise<State> => {
    const form = new FormData()
    form.set('type', type?.value ?? '')
    form.set('issue', target?.value ?? '')
    try {
      await api<Issue>(`${issuePath(workspace, issue.identifier)}/links`, form)
    } catch (e) {
      return toFormErrors(e)
    }
    setTarget(null)
    await reloadPage(router)
    return {}
  }, {})

  return (
    <form action={submit} noValidate className="grid gap-3">
      <div className="flex flex-wrap items-end gap-3">
        <div style={{ width: 160 }}>
          <SmartAutocomplete label="Link type" options={linkTypes} value={type} onChange={setType} error={errors?.type} />
        </div>
        <div className="flex-grow" style={{ minWidth: 200 }}>
          <SmartAutocomplete
            label="Issue"
            placeholder="Search issues"
            loadOptions={async (q, signal) => (await searchIssues(workspace, issue.identifier)(q, signal)).map(refOption)}
            subscribe={subscribeIssues(workspace)}
            value={target}
            onChange={setTarget}
            error={errors?.issue}
          />
        </div>
        <Button type="submit" variant="secondary" disabled={pending || !target}>
          {pending ? 'Adding…' : 'Add'}
        </Button>
      </div>
      <FormError>{error}</FormError>
    </form>
  )
}
