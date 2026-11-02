import { useEffect, useState } from 'react'
import { SmartAutocomplete } from '@tether/shared/smart-autocomplete'
import { api, projectPeoplePath, toFormErrors, type Label, type Member, type Project, type Status, type Workspace, workspacePath } from '@/api'
import { FormError } from '@/components/auth/auth-card'
import { CreateIssueForm, type Prefill } from '@/components/issues/issue-form'
import { subscribePaths } from '@/realtime'

type Data = { projects: Project[]; statuses: Status[]; labels: Label[]; members: Member[] }

// The last load per workspace, so reopening shows the form at once (at its full size) while it refetches.
const cache = new Map<string, Data>()

const storageKey = (slug: string) => `tether:last-project:${slug}`

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
  const [people, setPeople] = useState<{ key: string; members: Member[] } | null>(null)
  const path = `${workspacePath(workspace.slug)}`
  const secret = data?.projects.find((p) => p.key === key)?.private

  useEffect(
    () => subscribePaths([`${path}/projects`, `${path}/statuses`, `${path}/labels`, `${path}/members`], () => setVersion((v) => v + 1)),
    [path],
  )

  useEffect(() => {
    let live = true
    Promise.all([
      api<Project[]>(`${path}/projects`),
      api<Status[]>(`${path}/statuses`),
      api<Label[]>(`${path}/labels`),
      api<{ members: Member[] }>(`${path}/members`),
    ]).then(
      ([all, statuses, labels, { members }]) => {
        if (!live) return
        const projects = all.filter((p) => !p.archived)
        const keys = projects.map((p) => p.key)
        setKey((cur) => (cur && keys.includes(cur) ? cur : pick(keys)))
        const next = { projects, statuses, labels, members }
        cache.set(workspace.slug, next)
        setData(next)
      },
      (e) => live && setError(toFormErrors(e).error),
    )
    return () => {
      live = false
    }
  }, [path, workspace.slug, projectKey, version])

  useEffect(() => {
    if (!key || !secret) return
    let live = true
    api<Member[]>(projectPeoplePath(workspace.slug, key)).then(
      (members) => live && setPeople({ key, members }),
      (e) => live && setError(toFormErrors(e).error),
    )
    return () => {
      live = false
    }
  }, [workspace.slug, key, secret, version])

  // The loading text holds roughly the form's height, so the dialog doesn't jump when it arrives.
  if (!data) return error ? <FormError>{error}</FormError> : <p className="text-body-sm text-secondary" style={{ minHeight: 548 }}>Loading…</p>
  if (data.projects.length === 0) return <p className="text-body-sm text-secondary">No active projects. Create a project first.</p>

  const options = data.projects.map((p) => ({ value: p.key, label: p.name, hint: p.key }))
  const project = data.projects.find((p) => p.key === key)
  const members = project?.private ? (people?.key === project.key ? people.members : null) : data.members

  const picker = (
    <SmartAutocomplete
      label="Project"
      options={options}
      value={options.find((o) => o.value === key) ?? null}
      onChange={(o) => setKey(o?.value ?? null)}
    />
  )
  return (
    <div className="grid gap-4">
      <FormError>{error}</FormError>
      {project && members ? (
        <div onSubmit={() => rememberProject(workspace.slug, project.key)}>
          <CreateIssueForm workspace={workspace} project={project} projectPicker={picker} prefill={prefill} statuses={data.statuses} labels={data.labels} members={members} />
        </div>
      ) : (
        picker
      )}
    </div>
  )
}
