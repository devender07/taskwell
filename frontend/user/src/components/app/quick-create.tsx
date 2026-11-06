import { extractDataFromArray } from '@taskwell/shared/list'
import { useEffect, useState } from 'react'
import { SmartAutocomplete } from '@taskwell/shared/smart-autocomplete'
import { api, templatesPath, toFormErrors, type Label, type Project, type Status, type Template, type Workspace, workspacePath } from '@/api'
import { FormError } from '@/components/auth/auth-card'
import { CreateIssueForm, type Prefill } from '@/components/issues/issue-form'
import { subscribePaths } from '@/realtime'
import { peopleSearch } from '@/components/workspace/member-pages'
import { forProjectFirst } from '@/components/templates/template'

type Data = { projects: Project[]; statuses: Status[]; labels: Label[]; templates: Template[] }

// The last load per workspace, so reopening shows the form at once (at its full size) while it refetches.
const cache = new Map<string, Data>()

const storageKey = (slug: string) => `taskwell:last-project:${slug}`

function lastProject(slug: string) {
  try {
    return localStorage.getItem(storageKey(slug))
  } catch {
    return null
  }
}

function rememberProject(slug: string, key: string) {
  try {
    localStorage.setItem(storageKey(slug), key)
  } catch {}
}

export default function QuickCreate({ workspace, projectKey, prefill }: { workspace: Workspace; projectKey?: string; prefill?: Prefill }) {
  const [data, setData] = useState<Data | null>(() => cache.get(workspace.slug) ?? null)
  const [error, setError] = useState<string | null>(null)
  const pick = (keys: string[]) => [projectKey, lastProject(workspace.slug)].find((k) => k && keys.includes(k)) ?? keys[0] ?? null
  const [key, setKey] = useState<string | null>(() => (data ? pick(data.projects.map((p) => p.key)) : null))
  const [version, setVersion] = useState(0)
  const [picked, setPicked] = useState(false) // the user chose a project here, so a template's project doesn't replace it
  const path = `${workspacePath(workspace.slug)}`

  useEffect(
    () => subscribePaths([`${path}/projects`, `${path}/statuses`, `${path}/labels`, templatesPath(workspace.slug)], () => setVersion((v) => v + 1)),
    [path],
  )

  useEffect(() => {
    let live = true
    Promise.all([
      api<Project[]>(`${path}/projects`),
      api<Status[]>(`${path}/statuses`),
      api<Label[]>(`${path}/labels`),
      api<Template[]>(templatesPath(workspace.slug)).catch(() => []),
    ]).then(
      ([all, statuses, labels, templates]) => {
        if (!live) return
        const projects = all.filter((p) => !p.archived)
        const keys = projects.map((p) => p.key)
        setKey((cur) => (cur && keys.includes(cur) ? cur : pick(keys)))
        const next = { projects, statuses, labels, templates }
        cache.set(workspace.slug, next)
        setData(next)
      },
      (e) => live && setError(toFormErrors(e).error),
    )
    return () => {
      live = false
    }
  }, [path, workspace.slug, projectKey, version])

  // The loading text holds roughly the form's height, so the dialog doesn't jump when it arrives.
  if (!data) return error ? <FormError>{error}</FormError> : <p className="text-body-sm text-secondary" style={{ minHeight: 548 }}>Loading…</p>
  if (data.projects.length === 0) return <p className="text-body-sm text-secondary">No active projects. Create a project first.</p>

  const options = data.projects.map((p) => ({ value: p.key, label: p.name, hint: p.key }))
  const project = data.projects.find((p) => p.key === key)

  const picker = (
    <SmartAutocomplete
      label="Project"
      options={options}
      value={options.find((o) => o.value === key) ?? null}
      onChange={(o) => {
        setKey(extractDataFromArray(o, 'value', null))
        setPicked(true)
      }}
    />
  )
  return (
    <div className="grid gap-4">
      <FormError>{error}</FormError>
      {project ? (
        <div onSubmit={() => rememberProject(workspace.slug, project.key)}>
          <CreateIssueForm workspace={workspace} project={project} projectPicker={picker} prefill={prefill}
            statuses={data.statuses}
            labels={data.labels}
            people={peopleSearch(workspace.slug, project.key)}
            templates={forProjectFirst(data.templates, projectKey ?? project.key)}
            onTemplateProject={(k) => !picked && data.projects.some((p) => p.key === k) && setKey(k)}
          />
        </div>
      ) : (
        picker
      )}
    </div>
  )
}
