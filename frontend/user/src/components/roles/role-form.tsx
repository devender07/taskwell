import { useNavigate } from '@tanstack/react-router'
import { useActionState, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Checkbox } from '@taskwell/shared/checkbox'
import { Dialog } from '@taskwell/shared/dialog'
import { Input } from '@taskwell/shared/input'
import { roleName } from '@taskwell/shared/validate'
import { api, can, toFormErrors, type FieldErrors, type Permission, type Role, type Workspace, workspacePath } from '@/api'
import { FormError } from '@/components/auth/auth-card'

type State = { name: string; permissions: string[]; errors?: FieldErrors; error?: string | null }

// Create (no role) or edit a role. Admin is read-only; Member keeps its name.
export function RoleForm({ workspace, catalog, role }: { workspace: Workspace; catalog: Permission[]; role?: Role }) {
  const navigate = useNavigate()
  const readOnly = !can(workspace, 'roles.manage') || role?.builtin === 'admin'
  const base = `${workspacePath(workspace.slug)}/roles`

  const [{ name, permissions, errors, error }, submit, pending] = useActionState(async (_prev: State, form: FormData): Promise<State> => {
    const name = String(form.get('name') ?? '').trim()
    const permissions = form.getAll('permissions').map(String)
    const nameError = role?.builtin ? null : roleName(name)
    if (nameError) return { name, permissions, errors: { name: nameError } }
    form.set('name', name)
    try {
      await api<Role>(role ? `${base}/${encodeURIComponent(role.id)}` : base, form)
    } catch (e) {
      return { name, permissions, ...toFormErrors(e) }
    }
    await navigate({ to: '/$workspace/settings/roles', params: { workspace: workspace.slug } })
    return { name, permissions }
  }, { name: role?.name ?? '', permissions: role?.permissions ?? [] })

  // Guests can never hold workspace-wide permissions; the API rejects them too.
  const groups = [...new Set(catalog.map((p) => p.group))].filter((g) => role?.builtin !== 'guest' || !GUEST_HIDDEN_GROUPS.includes(g))

  return (
    <form action={submit} noValidate className="grid gap-6">
      <section className="grid gap-4 p-6 bg-surface border rounded-lg">
        <Input
          label="Role name"
          name="name"
          placeholder="Triage"
          autoComplete="off"
          readOnly={readOnly || Boolean(role?.builtin)}
          defaultValue={name}
          error={errors?.name}
        />
        {role?.builtin === 'admin' ? <p className="text-caption text-secondary">Admins always have every permission.</p> : null}
        {role?.builtin === 'member' ? <p className="text-caption text-secondary">People join as Member unless their invite says otherwise.</p> : null}
        {role?.builtin === 'guest' ? <p className="text-caption text-secondary">Guests see only the projects they're added to, and nothing workspace-wide.</p> : null}
      </section>

      <section className="grid gap-6 p-6 bg-surface border rounded-lg" aria-label="Permissions">
        {groups.map((group) => (
          <fieldset key={group} className="grid gap-3 m-0 p-0" style={{ border: 0 }}>
            <legend className="text-label text-primary mb-3 p-0">{group}</legend>
            {catalog.filter((p) => p.group === group).map((p) => (
              <div key={p.key} className="grid gap-1">
                <Checkbox
                  name="permissions"
                  value={p.key}
                  label={p.label}
                  defaultChecked={role?.builtin === 'admin' || permissions.includes(p.key)}
                  disabled={readOnly}
                />
                {p.active ? null : (
                  <span className="text-caption text-secondary" style={{ paddingLeft: 24, opacity: 0.7 }}>
                    Available when {group.toLowerCase()} ship
                  </span>
                )}
              </div>
            ))}
          </fieldset>
        ))}
        <FormError>{errors?.permissions}</FormError>
      </section>

      {readOnly ? (
        role?.builtin === 'admin' ? null : <p className="text-caption text-secondary">You don't have permission to change roles.</p>
      ) : (
        <>
          <FormError>{error}</FormError>
          <div className="flex items-center gap-3">
            <Button type="submit" disabled={pending}>
              {pending ? 'Saving…' : role ? 'Save changes' : 'Create role'}
            </Button>
          </div>
        </>
      )}
    </form>
  )
}

export function DeleteRole({ workspace, role }: { workspace: Workspace; role: Role }) {
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)

  const [error, remove, pending] = useActionState(async () => {
    try {
      await api(`${workspacePath(workspace.slug)}/roles/${encodeURIComponent(role.id)}`, undefined, 'DELETE')
    } catch (e) {
      return toFormErrors(e).error
    }
    await navigate({ to: '/$workspace/settings/roles', params: { workspace: workspace.slug }, replace: true })
    return null
  }, null)

  const count = role.member_count
  return (
    <section className="grid gap-4 p-6 border rounded-lg" style={{ borderColor: 'rgba(249, 24, 128, 0.4)' }}>
      <div className="grid gap-1">
        <h2 className="text-label">Delete role</h2>
        <p className="text-body-sm text-secondary">Its members and pending invites move to Member.</p>
      </div>
      <Button variant="danger" style={{ justifySelf: 'start' }} onClick={() => setOpen(true)}>
        Delete role
      </Button>

      <Dialog
        open={open}
        onClose={() => setOpen(false)}
        title={`Delete ${role.name}?`}
        description={
          count === 0
            ? 'No one has this role.'
            : `Its ${count} ${count === 1 ? 'member' : 'members'} will move to Member.`
        }
      >
        <form action={remove} className="grid gap-4">
          <FormError>{error}</FormError>
          <Button type="submit" variant="danger" className="w-full" disabled={pending}>
            {pending ? 'Deleting…' : 'Delete role'}
          </Button>
        </form>
      </Dialog>
    </section>
  )
}

const GUEST_HIDDEN_GROUPS = ['Workspace', 'Members', 'Analytics']
