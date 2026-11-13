import { Link, useNavigate, useRouter } from '@tanstack/react-router'
import { startTransition, useActionState, useState } from 'react'
import { Avatar } from '@taskwell/shared/avatar'
import { Button } from '@taskwell/shared/button'
import { Checkbox } from '@taskwell/shared/checkbox'
import { Dialog } from '@taskwell/shared/dialog'
import { IconButton } from '@taskwell/shared/icon-button'
import { LeaveIcon, UserMinusIcon } from '@taskwell/shared/icons'
import { Input } from '@taskwell/shared/input'
import { SmartAutocomplete, type Option } from '@taskwell/shared/smart-autocomplete'
import { email as checkEmail } from '@taskwell/shared/validate'
import { addProjectMembers, api, avatarSrc, removeProjectMember, can, homePath, toFormErrors, useFeature, type FieldErrors, type Invite, type Member, type Project, type Role, type Workspace, workspacePath } from '@/api'
import { FormError } from '@/components/auth/auth-card'
import { type MemberPage, useMemberPages } from './member-pages'
import '@/components/analytics/analytics.css'
import '@/components/projects/projects.css'
import { byId } from '@taskwell/shared/list'
import { DAY } from '@taskwell/shared/time'

type InviteState = { email: string; sent?: string; errors?: FieldErrors; error?: string | null }

// Without roles (e.g. on the Workspace page) invites join as Member. A guest invite also names the projects they join.
export function InviteForm({ workspace, roles, projects = [] }: { workspace: Workspace; roles?: Role[]; projects?: Pick<Project, 'key' | 'name'>[] }) {
  const slug = workspace.slug
  const router = useRouter()
  const guestRole = roles?.find((r) => r.builtin === 'guest')?.id
  const [roleId, setRoleId] = useState(roles?.find((r) => r.builtin === 'member')?.id)
  const guest = guestRole !== undefined && roleId === guestRole
  const [{ email, sent, errors, error }, submit, pending] = useActionState(async (_prev: InviteState, form: FormData): Promise<InviteState> => {
    const email = String(form.get('email') ?? '').trim()
    const fieldErrors = { email: checkEmail(email), project_keys: guest && !form.has('project_keys') ? 'Choose at least one project.' : null }
    if (fieldErrors.email || fieldErrors.project_keys) return { email, errors: fieldErrors }
    form.set('email', email)
    let invite: Invite
    try {
      invite = await api<Invite>(`${workspacePath(slug)}/invites`, form)
    } catch (e) {
      return { email, ...toFormErrors(e) }
    }
    await router.invalidate()
    return { email: '', sent: invite.email }
  }, { email: '' })

  return (
    <form action={submit} noValidate className="grid gap-4">
      <div className="grid gap-1">
        <h2 className="text-label">Invite people</h2>
        <p className="text-body-sm text-secondary">They'll get an email with a link to join {workspace.name}.</p>
      </div>
      <div className="flex items-start gap-3">
        <div className="flex-grow" style={{ minWidth: 0 }}>
          <Input
            aria-label="Email"
            name="email"
            type="email"
            autoComplete="off"
            placeholder="name@company.com"
            defaultValue={email}
            error={errors?.email}
          />
        </div>
        {roles ? (
          <div className="flex-noshrink" style={{ width: 160 }}>
            <SmartAutocomplete
              aria-label="Role"
              name="role_id"
              options={roleOptions(workspace, roles)}
              defaultValue={roleOptions(workspace, roles).find((o) => o.value === roles.find((r) => r.builtin === 'member')?.id)}
              onChange={(o) => setRoleId(o?.value)}
              error={errors?.role_id}
            />
          </div>
        ) : null}
        <Button type="submit" disabled={pending}>
          {pending ? 'Sending…' : 'Send invite'}
        </Button>
      </div>
      {guest ? (
        <fieldset className="grid gap-2 m-0 p-0" style={{ border: 0 }} aria-describedby="invite-projects-hint">
          <legend className="text-label p-0 mb-2">Projects</legend>
          <p id="invite-projects-hint" className="text-caption text-secondary m-0">Guests see only the projects you choose.</p>
          {projects.length ? (
            projects.map((p) => <Checkbox key={p.key} name="project_keys" value={p.key} label={p.name} />)
          ) : (
            <p className="text-body-sm text-secondary m-0">There are no projects to add a guest to yet.</p>
          )}
          <FormError>{errors?.project_keys}</FormError>
        </fieldset>
      ) : null}
      <FormError>{error}</FormError>
      {sent && !pending ? <p role="status" className="text-caption text-success">Invite sent to {sent}.</p> : null}
    </form>
  )
}

// Searchable by name or email; `page` is page one (the loader's for `query`), the rest load as you scroll.
export function MemberList({ workspace, roles, projects, page, userId, query, onQuery }: { workspace: Workspace; roles: Role[]; projects: Project[]; page: MemberPage; userId: string; query: string; onQuery: (q: string) => void }) {
  const { members, hasMore, end, error } = useMemberPages(workspace.slug, query.trim(), page)
  return (
    <section className="grid gap-3" aria-label="Members">
      <Input type="search" aria-label="Search members" placeholder="Search by name or email" defaultValue={query} onDebouncedChange={onQuery} />
      {members.length ? (
        <ul className="grid m-0 p-0 bg-surface border rounded-lg" style={{ listStyle: 'none' }}>
          {members.map((m) => (
            <MemberRow key={m.id} workspace={workspace} roles={roles} projects={projects} member={m} self={m.id === userId} />
          ))}
        </ul>
      ) : (
        <p className="text-body-sm text-secondary m-0">No one matches “{query.trim()}”.</p>
      )}
      <FormError>{error}</FormError>
      {hasMore ? <div ref={end} className="text-caption text-secondary text-center py-2">Loading more…</div> : null}
    </section>
  )
}

function MemberRow({ workspace, roles, projects, member, self }: { workspace: Workspace; roles: Role[]; projects: Project[]; member: Member; self: boolean }) {
  const router = useRouter()
  const team = useFeature('team')
  const navigate = useNavigate()
  const current = byId(roles, member.role.id)
  const manage = can(workspace, 'members.manage') && (self || (current !== undefined && grantable(workspace, current)))
  const path = `${workspacePath(workspace.slug)}/members/${encodeURIComponent(member.id)}`
  const options = roleOptions(workspace, roles)
  // Held until the reloaded member replaces it; useOptimistic dropped it a render early, flashing the old role.
  const [pendingRole, setPendingRole] = useState<string | null>(null)
  const [prevMember, setPrevMember] = useState(member)
  if (member !== prevMember) {
    setPrevMember(member)
    setPendingRole(null)
  }
  const roleId = pendingRole ?? member.role.id
  const [confirming, setConfirming] = useState(false)

  const [roleError, saveRole] = useActionState(async (_prev: string | null, form: FormData) => {
    try {
      await api<Member>(path, form)
    } catch (e) {
      setPendingRole(null)
      const { errors, error } = toFormErrors(e)
      return errors.role_id ?? error
    }
    await router.invalidate() // your own role change also changes what you can do
    return null
  }, null)

  const [removeError, remove, removing] = useActionState(async () => {
    try {
      await api(path, undefined, 'DELETE')
    } catch (e) {
      return toFormErrors(e).error
    }
    setConfirming(false)
    if (self) await navigate({ href: await homePath(), replace: true })
    else await router.invalidate()
    return null
  }, null)

  return (
    <li className="grid gap-2 px-6 py-3">
      <div className="flex flex-wrap items-center gap-3">
        <Avatar name={member.name} src={avatarSrc(member.id)} />
        <div className="grid flex-grow" style={{ minWidth: 0, flexBasis: 160 }}>
          <span className="text-label text-primary ellipsis">
            {team && (self || can(workspace, 'analytics.view')) ? (
              <Link to="/$workspace/members/$id" params={{ workspace: workspace.slug, id: member.id }} className="member-link">
                {member.name}
              </Link>
            ) : (
              member.name
            )}
            {self ? <span className="text-secondary font-400"> (you)</span> : null}
            {member.guest ? <span className="project-tag" style={{ marginLeft: 8 }}>Guest</span> : null}
          </span>
          <span className="text-caption text-secondary ellipsis">{member.email}</span>
        </div>
        {manage ? (
          <div className="flex-noshrink" style={{ width: 160 }}>
            <SmartAutocomplete
              aria-label={`Role for ${member.name}`}
              options={options}
              value={options.find((o) => o.value === roleId) ?? null}
              onChange={(o) => {
                if (!o || o.value === roleId) return
                const form = new FormData()
                form.set('role_id', o.value)
                setPendingRole(o.value)
                startTransition(() => saveRole(form))
              }}
            />
          </div>
        ) : (
          <span className="text-caption text-secondary flex-noshrink">{member.role.name}</span>
        )}
        {member.guest && manage && !self ? <GuestProjects workspace={workspace} projects={projects} member={member} /> : null}
        {self || manage ? (
          <IconButton
            className="icon-button-danger flex-noshrink"
            aria-label={self ? 'Leave workspace' : `Remove ${member.name}`}
            tooltip={self ? 'Leave workspace' : 'Remove member'}
            onClick={() => setConfirming(true)}
          >
            {self ? <LeaveIcon size={20} /> : <UserMinusIcon size={20} />}
          </IconButton>
        ) : null}
      </div>
      <FormError>{roleError}</FormError>

      <Dialog
        open={confirming}
        onClose={() => setConfirming(false)}
        title={self ? 'Leave workspace?' : 'Remove member?'}
        description={
          self ? (
            <>You'll lose access to <strong className="text-primary font-510">{workspace.name}</strong> until someone invites you again.</>
          ) : (
            <><strong className="text-primary font-510">{member.name}</strong> will lose access to {workspace.name}.</>
          )
        }
      >
        <form action={remove} className="grid gap-4">
          <FormError>{removeError}</FormError>
          <Button type="submit" variant="danger" className="w-full" disabled={removing}>
            {removing ? (self ? 'Leaving…' : 'Removing…') : self ? 'Leave workspace' : 'Remove member'}
          </Button>
        </form>
      </Dialog>
    </li>
  )
}

// Which projects a guest is in: checking a project adds them to it, unchecking removes them.
function GuestProjects({ workspace, projects, member }: { workspace: Workspace; projects: Project[]; member: Member }) {
  const router = useRouter()
  const [open, setOpen] = useState(false)
  const inProject = (p: Project) => p.members.some((m) => m.id === member.id)

  const [error, save, pending] = useActionState(async (_prev: string | null, form: FormData) => {
    const keys = new Set(form.getAll('project_keys').map(String))
    try {
      for (const p of projects) {
        if (keys.has(p.key) && !inProject(p)) await addProjectMembers(workspace.slug, p.key, [member.id])
        else if (!keys.has(p.key) && inProject(p)) await removeProjectMember(workspace.slug, p.key, member.id)
      }
    } catch (e) {
      await router.invalidate()
      return toFormErrors(e).error
    }
    await router.invalidate()
    setOpen(false)
    return null
  }, null)

  return (
    <>
      <Button variant="ghost" className="flex-noshrink" onClick={() => setOpen(true)} aria-label={`Projects for ${member.name}`}>
        Projects…
      </Button>
      <Dialog open={open} onClose={() => setOpen(false)} title="Projects" description={<><strong className="text-primary font-510">{member.name}</strong> sees only these projects.</>}>
        <form action={save} noValidate className="grid gap-4">
          {projects.length ? (
            <fieldset className="grid gap-2 m-0 p-0" style={{ border: 0 }}>
              <legend className="sr-only">Projects</legend>
              {projects.map((p) => (
                <Checkbox key={p.key} name="project_keys" value={p.key} label={p.name} defaultChecked={inProject(p)} />
              ))}
            </fieldset>
          ) : (
            <p className="text-body-sm text-secondary m-0">There are no projects yet.</p>
          )}
          <FormError>{error}</FormError>
          <Button type="submit" className="w-full" disabled={pending}>
            {pending ? 'Saving…' : 'Save'}
          </Button>
        </form>
      </Dialog>
    </>
  )
}

// Roles you can't grant (any permission you lack) are listed but disabled; the API enforces the same rule.
const grantable = (workspace: Workspace, role: Role) => role.permissions.every((p) => can(workspace, p))

const roleOptions = (workspace: Workspace, roles: Role[]): Option[] =>
  roles.map((r) => ({ value: r.id, label: r.name, disabled: !grantable(workspace, r) }))

export function PendingInvites({ slug, invites }: { slug: string; invites: Invite[] }) {
  return (
    <section className="grid gap-2">
      <h2 className="text-label">Pending invites</h2>
      <ul className="grid m-0 p-0 bg-surface border rounded-lg" style={{ listStyle: 'none' }}>
        {invites.map((invite) => (
          <PendingInvite key={invite.id} slug={slug} invite={invite} />
        ))}
      </ul>
    </section>
  )
}

function PendingInvite({ slug, invite }: { slug: string; invite: Invite }) {
  const router = useRouter()
  const path = `${workspacePath(slug)}/invites/${encodeURIComponent(invite.id)}`

  const [resent, resend, resending] = useActionState(async () => {
    try {
      await api<Invite>(`${path}/resend`, new FormData())
    } catch (e) {
      return { sent: false, error: toFormErrors(e).error }
    }
    await router.invalidate()
    return { sent: true, error: null }
  }, { sent: false, error: null as string | null })

  const [cancelError, cancel, cancelling] = useActionState(async () => {
    try {
      await api(path, undefined, 'DELETE')
    } catch (e) {
      return toFormErrors(e).error
    }
    await router.invalidate()
    return null
  }, null)

  const busy = resending || cancelling
  return (
    <li className="grid gap-2 px-6 py-3">
      <div className="flex items-center gap-3">
        <div className="grid flex-grow" style={{ minWidth: 0 }}>
          <span className="text-label text-primary ellipsis">{invite.email}</span>
          <span className="text-caption text-secondary ellipsis">{describe(invite)}</span>
        </div>
        {resent.sent && !busy ? <p role="status" className="text-caption text-success">Sent.</p> : null}
        <form action={resend}>
          <Button type="submit" variant="ghost" disabled={busy}>
            {resending ? 'Sending…' : 'Resend'}
          </Button>
        </form>
        <form action={cancel}>
          <Button type="submit" variant="ghost" disabled={busy}>
            {cancelling ? 'Cancelling…' : 'Cancel'}
          </Button>
        </form>
      </div>
      <FormError>{resent.error ?? cancelError}</FormError>
    </li>
  )
}


function describe(invite: Invite) {
  const days = Math.ceil((Date.parse(invite.expires_at) - Date.now()) / DAY)
  const expiry = days <= 0 ? 'Expired' : `expires in ${days} ${days === 1 ? 'day' : 'days'}`
  return invite.invited_by ? `${invite.role.name} · Invited by ${invite.invited_by} · ${expiry}` : `${invite.role.name} · ${expiry}`
}
