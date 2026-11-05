import { extractDataFromArray } from '@taskwell/shared/list'
import { useNavigate, useRouteContext, useRouter } from '@tanstack/react-router'
import { useActionState, useId, useState, type ReactNode } from 'react'
import { Avatar } from '@taskwell/shared/avatar'
import { Button } from '@taskwell/shared/button'
import { Checkbox } from '@taskwell/shared/checkbox'
import { DatePicker } from '@taskwell/shared/date-picker'
import { Dialog } from '@taskwell/shared/dialog'
import { Input } from '@taskwell/shared/input'
import { Textarea } from '@taskwell/shared/textarea'
import { projectDescription, projectKey, projectKeyFormat, projectName } from '@taskwell/shared/validate'
import { addProjectMembers, api, avatarSrc, removeProjectMember, toFormErrors, type FieldErrors, type PersonRef, type Project, type Workspace, workspacePath } from '@/api'
import { reloadPage } from '@/realtime'
import { FormError } from '@/components/auth/auth-card'
import { PeoplePicker } from '@/components/chat/chat-dialogs'

type State = { errors?: FieldErrors; error?: string | null }

const projectsPath = (workspace: Workspace) => `${workspacePath(workspace.slug)}/projects`

// Create (no project) or edit a project, then open it. The key follows the name until the user types in it.
export function ProjectForm({ workspace, project, onDone, aside }: { workspace: Workspace; project?: Project; onDone?: () => void; aside?: ReactNode }) {
  const formId = useId()
  const navigate = useNavigate()
  const router = useRouter()
  const [name, setName] = useState(extractDataFromArray(project, 'name', ''))
  const [editedKey, setEditedKey] = useState(extractDataFromArray(project, 'key', null))
  const [description, setDescription] = useState(extractDataFromArray(project, 'description', ''))
  const [secret, setSecret] = useState(extractDataFromArray(project, 'private', false))
  const [startDate, setStartDate] = useState(extractDataFromArray(project, 'start_date', ''))
  const [targetDate, setTargetDate] = useState(extractDataFromArray(project, 'target_date', ''))
  const [picked, setPicked] = useState<PersonRef[]>([])
  const { user } = useRouteContext({ from: '/_app' })
  const key = editedKey ?? projectKey(name)

  const [{ errors, error }, submit, pending] = useActionState(async (_prev: State, form: FormData): Promise<State> => {
    form.set('name', name.trim())
    form.set('key', key.trim().toUpperCase())
    form.set('description', description)
    form.set('private', String(secret))
    form.set('start_date', startDate)
    form.set('target_date', targetDate)
    if (secret && !project) for (const p of picked) form.append('member_ids', p.id)
    const errors = {
      name: projectName(name),
      key: projectKeyFormat(key),
      description: projectDescription(description),
      target_date: startDate && targetDate && targetDate < startDate ? 'Target date must be on or after the start date.' : null,
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

  const main = (
    <>
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
    </>
  )
  const side = (
    <>
      <div className="grid gap-4" style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(160px, 1fr))' }}>
        <DatePicker label="Start date" placeholder="No start date" value={startDate} onChange={setStartDate} error={errors?.start_date} />
        <DatePicker label="Target date" placeholder="No target date" value={targetDate} onChange={setTargetDate} error={errors?.target_date} />
      </div>
      <div className="grid gap-1">
        <Checkbox label="Private" checked={secret} onChange={(e) => setSecret(e.target.checked)} />
        <p className="text-caption text-secondary m-0">Only members and admins can see it</p>
      </div>
      {secret && !project ? <PeoplePicker slug={workspace.slug} value={picked} onChange={setPicked} exclude={[user.id]} error={errors?.member_ids} /> : null}
    </>
  )
  const label = project ? (pending ? 'Saving…' : 'Save changes') : pending ? 'Creating project…' : 'Create project'

  // With an aside (the edit dialog's milestones): name, key and description on the left, the rest on the right. The
  // aside has forms of its own, so it sits outside this form and the button submits it by id.
  if (aside)
    return (
      <div className="project-edit">
        <form id={formId} action={submit} noValidate className="project-edit-main">
          {main}
        </form>
        <div className="grid gap-4" style={{ alignContent: 'start' }}>
          {side}
          {aside}
        </div>
        <div className="project-edit-footer">
          <FormError>{error}</FormError>
          <Button type="submit" form={formId} disabled={pending}>
            {label}
          </Button>
        </div>
      </div>
    )

  return (
    <form action={submit} noValidate className="grid gap-4">
      {main}
      {side}
      <FormError>{error}</FormError>
      <Button type="submit" className="w-full" disabled={pending}>
        {label}
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
      <Button onClick={() => setOpen(true)}>
        Archive project
      </Button>
      <Dialog open={open} onClose={() => setOpen(false)} title={`Archive ${project.name}?`} description="Archived projects are hidden from the list. You can unarchive them later, or delete them for good.">
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

// Deleting takes an archived project and everything in it; typing its key confirms.
export function DeleteProject({ workspace, project }: { workspace: Workspace; project: Project }) {
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)
  const [typed, setTyped] = useState('')

  const [error, remove, pending] = useActionState(async () => {
    try {
      await api(`${projectsPath(workspace)}/${encodeURIComponent(project.key)}`, undefined, 'DELETE')
    } catch (e) {
      return toFormErrors(e).error
    }
    await navigate({ to: '/$workspace/projects', params: { workspace: workspace.slug }, replace: true })
    return null
  }, null)

  return (
    <>
      <Button variant="danger" onClick={() => setOpen(true)}>
        Delete project
      </Button>
      <Dialog
        open={open}
        onClose={() => setOpen(false)}
        title={`Delete ${project.name}?`}
        description="This deletes the project with all its issues, comments, cycles and milestones. It can't be undone."
      >
        <form action={remove} noValidate className="grid gap-4">
          <Input
            key={String(open)}
            label={`Type ${project.key} to confirm`}
            autoComplete="off"
            autoCapitalize="characters"
            spellCheck={false}
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
          />
          <FormError>{error}</FormError>
          <Button type="submit" variant="danger" className="w-full" disabled={pending || typed.trim().toUpperCase() !== project.key}>
            {pending ? 'Deleting…' : 'Delete project'}
          </Button>
        </form>
      </Dialog>
    </>
  )
}

// A private project's members: remove anyone, add from the workspace. Removing yourself hides it unless you're an admin.
// On a public project everyone sees it already, so this lists and adds only guests.
export function ProjectMembers({ workspace, project }: { workspace: Workspace; project: Project }) {
  const router = useRouter()
  const { user } = useRouteContext({ from: '/_app' })
  const [open, setOpen] = useState(false)
  const [picked, setPicked] = useState<PersonRef[]>([])
  const [removing, setRemoving] = useState<string | null>(null)
  const [removeError, setRemoveError] = useState<string | null>(null)

  const [{ errors, error }, add, pending] = useActionState(async (): Promise<State> => {
    if (!picked.length) return { errors: { user_ids: 'Add at least one person.' } }
    try {
      await addProjectMembers(workspace.slug, project.key, picked.map((p) => p.id))
    } catch (e) {
      return toFormErrors(e)
    }
    setPicked([])
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
        {project.private ? 'Members' : 'Guests'}
      </Button>
      <Dialog
        open={open}
        onClose={() => setOpen(false)}
        title={project.private ? 'Members' : 'Guests on this project'}
        description={project.private ? 'Only members and admins can see this project.' : 'Everyone in the workspace sees this project. Guests see it only when you add them here.'}
      >
        <div className="grid gap-5">
          {!project.private && !project.members.length ? <p className="text-body-sm text-secondary m-0">No guests yet.</p> : null}
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
            <PeoplePicker slug={workspace.slug} value={picked} onChange={setPicked} exclude={project.members.map((m) => m.id)} error={errors?.user_ids} guests={!project.private} />
            <FormError>{error}</FormError>
            <Button type="submit" disabled={pending} style={{ justifySelf: 'end' }}>
              {pending ? 'Adding…' : project.private ? 'Add people' : 'Add guests'}
            </Button>
          </form>
        </div>
      </Dialog>
    </>
  )
}
