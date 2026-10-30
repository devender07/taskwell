import { useNavigate, useRouter } from '@tanstack/react-router'
import { startTransition, useActionState, useOptimistic, useState } from 'react'
import { Avatar } from '@tether/shared/avatar'
import { Button } from '@tether/shared/button'
import { Dialog } from '@tether/shared/dialog'
import { Input } from '@tether/shared/input'
import { SmartAutocomplete, type Option } from '@tether/shared/smart-autocomplete'
import { email as checkEmail } from '@tether/shared/validate'
import { api, avatarSrc, can, toFormErrors, type FieldErrors, type Invite, type Member, type Role, type Workspace } from '@/api'
import { FormError } from '@/components/auth/auth-card'

type InviteState = { email: string; sent?: string; errors?: FieldErrors; error?: string | null }

// Without roles (e.g. on the Workspace page) invites join as Member.
export function InviteForm({ workspace, roles }: { workspace: Workspace; roles?: Role[] }) {
  const slug = workspace.slug
  const router = useRouter()
  const [{ email, sent, errors, error }, submit, pending] = useActionState(async (_prev: InviteState, form: FormData): Promise<InviteState> => {
    const email = String(form.get('email') ?? '').trim()
    const fieldError = checkEmail(email)
    if (fieldError) return { email, errors: { email: fieldError } }
    form.set('email', email)
    let invite: Invite
    try {
      invite = await api<Invite>(`/workspaces/${encodeURIComponent(slug)}/invites`, form)
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
              error={errors?.role_id}
            />
          </div>
        ) : null}
        <Button type="submit" disabled={pending}>
          {pending ? 'Sending…' : 'Send invite'}
        </Button>
      </div>
      <FormError>{error}</FormError>
      {sent && !pending ? <p role="status" className="text-caption text-success">Invite sent to {sent}.</p> : null}
    </form>
  )
}

export function MemberList({ workspace, roles, members, userId }: { workspace: Workspace; roles: Role[]; members: Member[]; userId: string }) {
  return (
    <section className="grid bg-surface border rounded-lg" aria-label="Members">
      <ul className="grid m-0 p-0" style={{ listStyle: 'none' }}>
        {members.map((m) => (
          <MemberRow key={m.id} workspace={workspace} roles={roles} member={m} self={m.id === userId} />
        ))}
      </ul>
    </section>
  )
}

function MemberRow({ workspace, roles, member, self }: { workspace: Workspace; roles: Role[]; member: Member; self: boolean }) {
  const router = useRouter()
  const navigate = useNavigate()
  const current = roles.find((r) => r.id === member.role.id)
  const manage = can(workspace, 'members.manage') && (self || (current !== undefined && grantable(workspace, current)))
  const path = `/workspaces/${encodeURIComponent(workspace.slug)}/members/${encodeURIComponent(member.id)}`
  const options = roleOptions(workspace, roles)
  const [roleId, setRoleId] = useOptimistic(member.role.id)
  const [confirming, setConfirming] = useState(false)

  const [roleError, saveRole] = useActionState(async (_prev: string | null, form: FormData) => {
    setRoleId(String(form.get('role_id')))
    try {
      await api<Member>(path, form)
    } catch (e) {
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
    if (self) await navigate({ to: '/', replace: true })
    else await router.invalidate()
    return null
  }, null)

  return (
    <li className="grid gap-2 px-6 py-3">
      <div className="flex items-center gap-3">
        <Avatar name={member.name} src={avatarSrc(member.id)} />
        <div className="grid flex-grow" style={{ minWidth: 0 }}>
          <span className="text-label text-primary ellipsis">
            {member.name}
            {self ? <span className="text-secondary font-400"> (you)</span> : null}
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
                startTransition(() => saveRole(form))
              }}
            />
          </div>
        ) : (
          <span className="text-caption text-secondary flex-noshrink">{member.role.name}</span>
        )}
        {self || manage ? (
          <Button variant="ghost" className="flex-noshrink" onClick={() => setConfirming(true)}>
            {self ? 'Leave workspace' : 'Remove'}
          </Button>
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
  const path = `/workspaces/${encodeURIComponent(slug)}/invites/${encodeURIComponent(invite.id)}`

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

const DAY = 24 * 60 * 60 * 1000

function describe(invite: Invite) {
  const days = Math.ceil((Date.parse(invite.expires_at) - Date.now()) / DAY)
  const expiry = days <= 0 ? 'Expired' : `expires in ${days} ${days === 1 ? 'day' : 'days'}`
  return invite.invited_by ? `${invite.role.name} · Invited by ${invite.invited_by} · ${expiry}` : `${invite.role.name} · ${expiry}`
}
