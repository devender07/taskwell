import { extractDataFromArray } from '@taskwell/shared/list'
import { Link, useRouter } from '@tanstack/react-router'
import { startTransition, useActionState, useId, useRef, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Checkbox } from '@taskwell/shared/checkbox'
import { Dialog } from '@taskwell/shared/dialog'
import { Input } from '@taskwell/shared/input'
import { SmartAutocomplete, type Option } from '@taskwell/shared/smart-autocomplete'
import { Tabs } from '@taskwell/shared/tabs'
import { filled } from '@taskwell/shared/validate'
import { api, toFormErrors, workspacePath, type FieldErrors, type ImportSource, type ImportSummary, type Project, type Workspace } from '@/api'
import { reloadPage } from '@/realtime'
import { FormError } from '@/components/auth/auth-card'

type Tab = 'csv' | 'github'
type Mode = 'preview' | 'import'
// version: the inputs the summary was made from; any edit after it hides the preview.
type State = { errors?: FieldErrors; error?: string | null; preview?: ImportSummary; done?: ImportSummary; version?: number }

const tabs: { value: Tab; label: string }[] = [
  { value: 'csv', label: 'CSV' },
  { value: 'github', label: 'GitHub' },
]
const SOURCE: Record<ImportSource, string> = { linear: 'Linear', jira: 'Jira', csv: 'CSV', github: 'GitHub' }
const issues = (n: number) => `${n.toLocaleString()} ${n === 1 ? 'issue' : 'issues'}`
const SKIPPED_SHOWN = 20

// "Import issues" button and its dialog, on the projects list and a project page (which passes only itself).
export function ImportButton({ workspace, projects, project, variant = 'secondary' }: { workspace: Workspace; projects: Project[]; project?: string; variant?: 'primary' | 'secondary' | 'ghost' }) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <Button variant={variant} onClick={() => setOpen(true)}>
        Import issues
      </Button>
      <Dialog
        open={open}
        onClose={() => setOpen(false)}
        title="Import issues"
        description="From a CSV export or a GitHub repository. Preview first; nothing changes until you import."
        wide
      >
        {open ? <ImportForm workspace={workspace} projects={projects} project={project} /> : null}
      </Dialog>
    </>
  )
}

// Imports a CSV or a GitHub repository's issues into a project: "Preview" is a dry run, then "Import" does it.
export function ImportForm({ workspace, projects, project }: { workspace: Workspace; projects: Project[]; project?: string }) {
  const projectOptions = projects.filter((p) => !p.archived).map((p) => ({ value: p.key, label: p.name, hint: p.key }))
  const [tab, setTab] = useState<Tab>('csv')
  const [file, setFile] = useState<File | null>(null)
  const [target, setTarget] = useState<Option | null>(projectOptions.find((o) => o.value === project) ?? null)
  const [repo, setRepo] = useState('')
  const [token, setToken] = useState('')
  const [closed, setClosed] = useState(false)
  const [version, setVersion] = useState(0)
  const router = useRouter()
  const [dismissed, setDismissed] = useState<ImportSummary>()
  const picker = useRef<HTMLInputElement>(null)
  const fileId = useId()
  const edit = <T,>(set: (v: T) => void) => (v: T) => (set(v), setVersion((n) => n + 1))

  const [state, submit, pending] = useActionState(async (_prev: State, mode: Mode): Promise<State> => {
    const errors =
      tab === 'csv'
        ? { file: filled('Choose a CSV file.')(extractDataFromArray(file, 'name', '')), project: filled('Choose a project.')(target?.value ?? '') }
        : { repo: filled('Enter a repository like owner/name.')(repo), project: filled('Choose a project.')(target?.value ?? '') }
    if (Object.values(errors).some(Boolean)) return { errors }
    const form = new FormData()
    if (tab === 'csv' && file) form.set('file', file)
    if (tab === 'github') {
      form.set('repo', repo.trim())
      if (token.trim()) form.set('token', token.trim())
      if (closed) form.set('closed', '1')
    }
    form.set('project', target?.value ?? '')
    if (mode === 'preview') form.set('dry_run', '1')
    try {
      const summary = await api<ImportSummary>(`${workspacePath(workspace.slug)}/import/${tab}`, form)
      if (mode === 'preview') return { preview: summary, version }
      await reloadPage(router)
      return { done: summary }
    } catch (e) {
      return toFormErrors(e)
    }
  }, {})
  const { errors, error, done } = state
  const preview = state.version === version ? state.preview : undefined
  const ready = preview ? preview.total - preview.skipped.length : 0
  const run = (mode: Mode) => startTransition(() => submit(mode))

  if (done && done !== dismissed && done.imported !== undefined) {
    const key = target?.value ?? ''
    return (
      <section className="grid gap-4 p-6 bg-surface border rounded-lg" aria-live="polite">
        <h2 className="text-h2">Imported {issues(done.imported)}</h2>
        <p className="text-body-sm text-secondary">
          {done.first ? `Numbering starts at ${done.first}. ` : ''}
          <Link to="/$workspace/projects/$key" params={{ workspace: workspace.slug, key }} style={{ color: 'var(--link)' }}>
            Open {target?.label ?? 'the project'}
          </Link>
        </p>
        <Button
          variant="secondary"
          className="w-fit"
          onClick={() => {
            setDismissed(done)
            setFile(null)
            setVersion((n) => n + 1)
          }}
        >
          Import more
        </Button>
      </section>
    )
  }

  return (
    <form noValidate action={() => run('preview')} className="grid gap-5" style={{ maxWidth: 560 }}>
      <Tabs aria-label="Import from" className="w-fit" items={tabs} value={tab} onChange={edit(setTab)} />
      {tab === 'csv' ? (
        <div className="grid gap-2">
          <span id={`${fileId}-label`} className="text-label text-secondary">CSV file</span>
          <div className="flex flex-wrap items-center gap-3" style={{ minWidth: 0 }}>
            <Button
              variant="secondary"
              onClick={() => picker.current?.click()}
              aria-labelledby={`${fileId}-label ${fileId}-button`}
              id={`${fileId}-button`}
              aria-describedby={errors?.file ? `${fileId}-error` : undefined}>
              Choose file
            </Button>
            <span className="text-body-sm ellipsis" style={{ minWidth: 0 }}>{file ? file.name : 'No file chosen'}</span>
          </div>
          <input
            ref={picker}
            type="file"
            accept=".csv,text/csv"
            hidden
            onChange={(e) => {
              const f = e.currentTarget.files?.[0]
              e.currentTarget.value = ''
              if (f) edit(setFile)(f)
            }}
          />
          {errors?.file ? <span id={`${fileId}-error`} className="text-caption text-accent">{errors.file}</span> : null}
          <p className="text-caption text-secondary">
            Export from Linear (Settings → Import/export) or Jira (Filters → Export CSV), or any CSV with a Title column.
          </p>
        </div>
      ) : (
        <>
          <Input label="Repository" placeholder="owner/name" autoComplete="off" value={repo} onChange={(e) => edit(setRepo)(e.target.value)} error={errors?.repo} />
          <div className="grid gap-2">
            <Input label="Token" type="password" autoComplete="off" value={token} onChange={(e) => edit(setToken)(e.target.value)} error={errors?.token} />
            <p className="text-caption text-secondary">Needed for private repositories. Used once, never stored.</p>
          </div>
          <Checkbox label="Include closed issues" checked={closed} onChange={(e) => edit(setClosed)(e.target.checked)} />
        </>
      )}
      <SmartAutocomplete label="Project" placeholder="Choose a project" options={projectOptions} value={target} onChange={edit(setTarget)} error={errors?.project} />
      <FormError>{error}</FormError>
      {preview ? <Preview summary={preview} /> : null}
      <div className="flex flex-wrap gap-3">
        <Button type="submit" variant={preview ? 'secondary' : 'primary'} disabled={pending}>
          {pending && !preview ? 'Previewing…' : 'Preview'}
        </Button>
        {preview && ready > 0 ? (
          <Button onClick={() => run('import')} disabled={pending}>
            {pending ? 'Importing…' : `Import ${issues(ready)}`}
          </Button>
        ) : null}
      </div>
    </form>
  )
}

function Preview({ summary: s }: { summary: ImportSummary }) {
  const more = s.skipped.length - SKIPPED_SHOWN
  return (
    <section className="grid gap-4 p-6 bg-surface border rounded-lg" aria-label="Preview" aria-live="polite" style={{ minWidth: 0 }}>
      <h2 className="text-label">
        {issues(s.total)} from {SOURCE[s.source]}
      </h2>
      {s.statuses.length ? (
        <div style={{ overflowX: 'auto' }}>
          <table className="text-body-sm" style={{ width: '100%', borderCollapse: 'collapse' }}>
            <thead>
              <tr className="text-label text-secondary">
                <th style={{ textAlign: 'left', padding: '0 16px 8px 0' }}>Status in file</th>
                <th style={{ textAlign: 'left', padding: '0 16px 8px 0' }}>Becomes</th>
                <th style={{ textAlign: 'right', padding: '0 0 8px' }}>Issues</th>
              </tr>
            </thead>
            <tbody>
              {s.statuses.map((m) => (
                <tr key={m.from} style={{ borderTop: '1px solid var(--border)' }}>
                  <td style={{ padding: '8px 16px 8px 0', overflowWrap: 'anywhere' }}>{m.from || 'None'}</td>
                  <td style={{ padding: '8px 16px 8px 0' }}>{m.to.name}</td>
                  <td style={{ padding: '8px 0', textAlign: 'right' }}>{m.count}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
      {s.new_labels.length ? <p className="text-body-sm" style={{ overflowWrap: 'anywhere' }}>New labels: {s.new_labels.join(', ')}</p> : null}
      {s.unmatched_assignees.length ? (
        <p className="text-body-sm" style={{ overflowWrap: 'anywhere' }}>Not matched, left unassigned: {s.unmatched_assignees.join(', ')}</p>
      ) : null}
      <p className="text-body-sm">{s.comments.toLocaleString()} {s.comments === 1 ? 'comment' : 'comments'}</p>
      {s.skipped.length ? (
        <div className="grid gap-2">
          <h3 className="text-label text-secondary">Skipped</h3>
          <ul className="grid gap-1 m-0 p-0 text-body-sm" style={{ listStyle: 'none' }}>
            {s.skipped.slice(0, SKIPPED_SHOWN).map((r) => (
              <li key={r.row}>
                {s.source === 'github' ? `#${r.row}` : `Row ${r.row}`}: {r.error}
              </li>
            ))}
            {more > 0 ? <li className="text-secondary">and {more} more</li> : null}
          </ul>
        </div>
      ) : null}
    </section>
  )
}
