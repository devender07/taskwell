import { useEffect, useState } from 'react'
import { SmartAutocomplete } from '@tether/shared/smart-autocomplete'
import { api, toFormErrors, type Label, type Member, type Project, type Status, type Workspace } from '@/api'
import { FormError } from '@/components/auth/auth-card'
import { CreateIssueForm } from '@/components/issues/issue-form'

type Data = { projects: Project[]; statuses: Status[]; labels: Label[]; members: Member[] }

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

export default function QuickCreate({ workspace, projectKey }: { workspace: Workspace; projectKey?: string }) {
  const [data, setData] = useState<Data | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [key, setKey] = useState<string | null>(null)

  useEffect(() => {
    let live = true
    const path = `/workspaces/${encodeURIComponent(workspace.slug)}`
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
        setKey([projectKey, lastProject(workspace.slug)].find((k) => k && keys.includes(k)) ?? keys[0] ?? null)
        setData({ projects, statuses, labels, members })
      },
      (e) => live && setError(toFormErrors(e).error),
    )
    return () => {
      live = false
    }
  }, [workspace.slug, projectKey])

  if (error) return <FormError>{error}</FormError>
  if (!data) return <p className="text-body-sm text-secondary">Loading…</p>
  if (data.projects.length === 0) return <p className="text-body-sm text-secondary">No active projects. Create a project first.</p>

  const options = data.projects.map((p) => ({ value: p.key, label: p.name, hint: p.key }))
  const project = data.projects.find((p) => p.key === key)

  return (
    <div className="grid gap-4">
      <SmartAutocomplete
        label="Project"
        options={options}
        value={options.find((o) => o.value === key) ?? null}
        onChange={(o) => setKey(o?.value ?? null)}
      />
      {project ? (
        <div onSubmit={() => rememberProject(workspace.slug, project.key)}>
          <CreateIssueForm workspace={workspace} project={project} statuses={data.statuses} labels={data.labels} members={data.members} />
        </div>
      ) : null}
    </div>
  )
}
