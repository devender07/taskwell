import { useNavigate, useRouter } from '@tanstack/react-router'
import { useActionState, useState } from 'react'
import { Button } from '@tether/shared/button'
import { Dialog } from '@tether/shared/dialog'
import { Input } from '@tether/shared/input'
import { Textarea } from '@tether/shared/textarea'
import { projectDescription, projectKey, projectKeyFormat, projectName } from '@tether/shared/validate'
import { api, toFormErrors, type FieldErrors, type Project, type Workspace } from '@/api'
import { FormError } from '@/components/auth/auth-card'

type State = { errors?: FieldErrors; error?: string | null }

const projectsPath = (workspace: Workspace) => `/workspaces/${encodeURIComponent(workspace.slug)}/projects`

// Create (no project) or edit a project, then open it. The key follows the name until the user types in it.
export function ProjectForm({ workspace, project, onDone }: { workspace: Workspace; project?: Project; onDone?: () => void }) {
  const navigate = useNavigate()
  const router = useRouter()
  const [name, setName] = useState(project?.name ?? '')
  const [editedKey, setEditedKey] = useState(project?.key ?? null)
  const [description, setDescription] = useState(project?.description ?? '')
  const key = editedKey ?? projectKey(name)

  const [{ errors, error }, submit, pending] = useActionState(async (_prev: State, form: FormData): Promise<State> => {
    form.set('name', name.trim())
    form.set('key', key.trim().toUpperCase())
    form.set('description', description)
    const errors = {
      name: projectName(name),
      key: projectKeyFormat(key),
      description: projectDescription(description),
    }
    if (Object.values(errors).some(Boolean)) return { errors }
    let saved: Project
    try {
      saved = await api<Project>(project ? `${projectsPath(workspace)}/${encodeURIComponent(project.key)}` : projectsPath(workspace), form)
    } catch (e) {
      return toFormErrors(e)
    }
    if (saved.key === project?.key) await router.invalidate()
    else await navigate({ to: '/$workspace/projects/$key', params: { workspace: workspace.slug, key: saved.key }, replace: Boolean(project) })
    onDone?.()
    return {}
  }, {})

  return (
    <form action={submit} noValidate className="grid gap-4">
      <Input label="Project name" name="name" placeholder="Engineering" autoComplete="off" autoFocus value={name} onChange={(e) => setName(e.target.value)} error={errors?.name} />
      <Input
        label="Project key"
        name="key"
        placeholder="ENG"
        autoComplete="off"
        autoCapitalize="characters"
        spellCheck={false}
        value={key}
        onChange={(e) => setEditedKey(e.target.value.toUpperCase())}
        error={errors?.key}
      />
      <Textarea
        label="Description"
        name="description"
        placeholder="What this project is for"
        value={description}
        onChange={(e) => setDescription(e.target.value)}
        error={errors?.description}
      />
      <FormError>{error}</FormError>
      <Button type="submit" className="w-full" disabled={pending}>
        {project ? (pending ? 'Saving…' : 'Save changes') : pending ? 'Creating project…' : 'Create project'}
      </Button>
    </form>
  )
}

// Archive asks first; unarchive is one click.
export function ArchiveProject({ workspace, project }: { workspace: Workspace; project: Project }) {
  const router = useRouter()
  const [open, setOpen] = useState(false)

  const [error, toggle, pending] = useActionState(async () => {
    try {
      await api<Project>(`${projectsPath(workspace)}/${encodeURIComponent(project.key)}/${project.archived ? 'unarchive' : 'archive'}`, new FormData())
    } catch (e) {
      return toFormErrors(e).error
    }
    await router.invalidate()
    setOpen(false)
    return null
  }, null)

  if (project.archived) {
    return (
      <form action={toggle} className="grid gap-2">
        <Button type="submit" variant="secondary" disabled={pending}>
          {pending ? 'Unarchiving…' : 'Unarchive project'}
        </Button>
        <FormError>{error}</FormError>
      </form>
    )
  }

  return (
    <>
      <Button variant="secondary" onClick={() => setOpen(true)}>
        Archive project
      </Button>
      <Dialog open={open} onClose={() => setOpen(false)} title={`Archive ${project.name}?`} description="Archived projects are hidden from the list. You can unarchive them later.">
        <form action={toggle} className="grid gap-4">
          <FormError>{error}</FormError>
          <Button type="submit" className="w-full" disabled={pending}>
            {pending ? 'Archiving…' : 'Archive project'}
          </Button>
        </form>
      </Dialog>
    </>
  )
}
