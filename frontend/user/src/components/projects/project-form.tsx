import { useNavigate, useRouteContext, useRouter } from '@tanstack/react-router'
import { useActionState, useEffect, useState } from 'react'
import { Avatar } from '@tether/shared/avatar'
import { Button } from '@tether/shared/button'
import { Checkbox } from '@tether/shared/checkbox'
import { Dialog } from '@tether/shared/dialog'
import { Input } from '@tether/shared/input'
import { Textarea } from '@tether/shared/textarea'
import { projectDescription, projectKey, projectKeyFormat, projectName } from '@tether/shared/validate'
import { addProjectMembers, api, avatarSrc, removeProjectMember, toFormErrors, type FieldErrors, type Member, type PersonRef, type Project, type Workspace } from '@/api'
import { reloadPage } from '@/realtime'
import { FormError } from '@/components/auth/auth-card'
import { PeoplePicker } from '@/components/chat/chat-dialogs'

type State = { errors?: FieldErrors; error?: string | null }

const projectsPath = (workspace: Workspace) => `/workspaces/${encodeURIComponent(workspace.slug)}/projects`

// The whole workspace, for picking a private project's members; fetched only while a picker is shown.
function useWorkspaceMembers(workspace: Workspace, enabled: boolean) {
  const [members, setMembers] = useState<Member[]>([])
  useEffect(() => {
    if (!enabled) return
    let live = true
    api<{ members: Member[] }>(`/workspaces/${encodeURIComponent(workspace.slug)}/members`).then(
      (r) => live && setMembers(r.members),
      () => {},
    )
    return () => {
      live = false
    }
  }, [workspace.slug, enabled])
  return members
}

// Create (no project) or edit a project, then open it. The key follows the name until the user types in it.
export function ProjectForm({ workspace, project, onDone }: { workspace: Workspace; project?: Project; onDone?: () => void }) {
  const navigate = useNavigate()
  const router = useRouter()
  const [name, setName] = useState(project?.name ?? '')
  const [editedKey, setEditedKey] = useState(project?.key ?? null)
  const [description, setDescription] = useState(project?.description ?? '')
  const [secret, setSecret] = useState(project?.private ?? false)
  const [memberIds, setMemberIds] = useState<string[]>([])
  const { user } = useRouteContext({ from: '/_app' })
  const members = useWorkspaceMembers(workspace, secret && !project)
  const key = editedKey ?? projectKey(name)

  const [{ errors, error }, submit, pending] = useActionState(async (_prev: State, form: FormData): Promise<State> => {
    form.set('name', name.trim())
    form.set('key', key.trim().toUpperCase())
    form.set('description', description)
    form.set('private', String(secret))
    if (secret && !project) for (const id of memberIds) form.append('member_ids', id)
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
    if (saved.key === project?.key) await reloadPage(router)
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
      <div className="grid gap-1">
        <Checkbox label="Private" checked={secret} onChange={(e) => setSecret(e.target.checked)} />
        <p className="text-caption text-secondary m-0">Only members and admins can see it</p>
      </div>
      {secret && !project ? <PeoplePicker members={members} value={memberIds} onChange={setMemberIds} exclude={[user.id]} error={errors?.member_ids} /> : null}
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
    await reloadPage(router)
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

// A private project's members: remove anyone, add from the workspace. Removing yourself hides it unless you're an admin.
export function ProjectMembers({ workspace, project }: { workspace: Workspace; project: Project }) {
  const router = useRouter()
  const { user } = useRouteContext({ from: '/_app' })
  const [open, setOpen] = useState(false)
  const [ids, setIds] = useState<string[]>([])
  const [removing, setRemoving] = useState<string | null>(null)
  const [removeError, setRemoveError] = useState<string | null>(null)
  const members = useWorkspaceMembers(workspace, open)

  const [{ errors, error }, add, pending] = useActionState(async (): Promise<State> => {
    if (!ids.length) return { errors: { user_ids: 'Add at least one person.' } }
    try {
      await addProjectMembers(workspace.slug, project.key, ids)
    } catch (e) {
      return toFormErrors(e)
    }
    setIds([])
    await reloadPage(router)
    return {}
  }, {})

  async function remove(p: PersonRef) {
    setRemoving(p.id)
    setRemoveError(null)
    try {
      await removeProjectMember(workspace.slug, project.key, p.id)
      await reloadPage(router)
    } catch (e) {
      setRemoveError(toFormErrors(e).error)
    }
    setRemoving(null)
  }

  return (
    <>
      <Button variant="secondary" onClick={() => setOpen(true)}>
        Members
      </Button>
      <Dialog open={open} onClose={() => setOpen(false)} title="Members" description="Only members and admins can see this project.">
        <div className="grid gap-5">
          <ul className="grid gap-1 m-0 p-0" style={{ listStyle: 'none' }}>
            {project.members.map((p) => (
              <li key={p.id} className="flex items-center gap-3" style={{ minHeight: 44 }}>
                <Avatar name={p.name} size={24} src={avatarSrc(p.id)} />
                <span className="text-body-sm flex-grow ellipsis">
                  {p.name}
                  {p.id === user.id ? <span className="text-secondary"> (you)</span> : null}
                </span>
                <Button variant="ghost" disabled={removing === p.id} onClick={() => remove(p)} aria-label={`Remove ${p.name}`}>
                  {removing === p.id ? 'Removing…' : 'Remove'}
                </Button>
              </li>
            ))}
          </ul>
          <FormError>{removeError}</FormError>
          <form action={add} noValidate className="grid gap-4">
            <PeoplePicker members={members} value={ids} onChange={setIds} exclude={project.members.map((m) => m.id)} error={errors?.user_ids} />
            <FormError>{error}</FormError>
            <Button type="submit" disabled={pending} style={{ justifySelf: 'end' }}>
              {pending ? 'Adding…' : 'Add people'}
            </Button>
          </form>
        </div>
      </Dialog>
    </>
  )
}
