import { useRouter } from '@tanstack/react-router'
import { useActionState, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Checkbox } from '@taskwell/shared/checkbox'
import { Dialog } from '@taskwell/shared/dialog'
import { ChevronDownIcon } from '@taskwell/shared/icons'
import { Input } from '@taskwell/shared/input'
import { roleName } from '@taskwell/shared/validate'
import { saveDefault, toFormErrors, type DefaultRole, type FieldErrors, type Permission } from '@/api'
import { ConfirmDelete, FormError, Section } from './parts'

const GUEST_DENIED_GROUPS = ['Workspace', 'Members', 'Analytics']

const NOTES = {
  member: 'People join as Member unless their invite says otherwise.',
  guest: "Guests see only the projects they're added to, and nothing workspace-wide.",
}

const count = (n: number) => `${n} ${n === 1 ? 'permission' : 'permissions'}`
const summary = { minHeight: 56, cursor: 'pointer' } as const

export function Roles({ roles, catalog }: { roles: DefaultRole[]; catalog: Permission[] }) {
  const [adding, setAdding] = useState(false)
  return (
    <Section
      title="Roles"
      description="New workspaces start with these roles. Admin is always added with every permission."
      action={<Button variant="secondary" onClick={() => setAdding(true)}>Add role</Button>}
    >
      <div className="grid gap-2">
        <div className="flex items-center gap-3 px-6 bg-surface border rounded-lg" style={{ minHeight: 56 }}>
          <span className="text-label text-primary flex-grow">Admin</span>
          <span className="text-caption text-secondary">All permissions</span>
        </div>
        {roles.map((r) => (
          <details key={r.id} className="defaults-role bg-surface border rounded-lg">
            <summary className="flex items-center gap-3 px-6" style={summary}>
              <span className="text-label text-primary flex-grow ellipsis">{r.name}</span>
              <span className="text-caption text-secondary">{count(r.permissions.length)}</span>
              <ChevronDownIcon size={16} className="text-secondary" />
            </summary>
            <div className="grid gap-6 p-6" style={{ borderTop: '1px solid var(--border)' }}>
              <RoleForm key={`${r.name}:${r.permissions.join()}`} role={r} catalog={catalog} />
              {r.builtin ? null : (
                <div>
                  <ConfirmDelete kind="roles" id={r.id} name={r.name} noun="role" description="New workspaces won't get this role. Existing workspaces keep theirs." />
                </div>
              )}
            </div>
          </details>
        ))}
      </div>
      <Dialog open={adding} onClose={() => setAdding(false)} title="Add role" description="New workspaces get it alongside Member and Guest.">
        <RoleForm key={String(adding)} catalog={catalog} onDone={() => setAdding(false)} />
      </Dialog>
    </Section>
  )
}

type State = { name: string; permissions: string[]; errors?: FieldErrors; error?: string | null }

function RoleForm({ role, catalog, onDone }: { role?: DefaultRole; catalog: Permission[]; onDone?: () => void }) {
  const router = useRouter()
  const builtin = role?.builtin ?? null

  const [{ name, permissions, errors, error }, submit, pending] = useActionState(async (_prev: State, form: FormData): Promise<State> => {
    const name = String(form.get('name') ?? '').trim()
    const permissions = form.getAll('permission').map(String)
    const nameError = builtin ? null : roleName(name)
    if (nameError) return { name, permissions, errors: { name: nameError } }
    if (builtin) form.delete('name')
    else form.set('name', name)
    try {
      await saveDefault<DefaultRole>('roles', form, role?.id)
    } catch (e) {
      return { name, permissions, ...toFormErrors(e) }
    }
    await router.invalidate()
    onDone?.()
    return { name, permissions }
  }, { name: role?.name ?? '', permissions: role?.permissions ?? [] })

  const groups = [...new Set(catalog.map((p) => p.group))]

  return (
    <form action={submit} noValidate className="grid gap-6">
      {builtin ? (
        <p className="text-caption text-secondary m-0">{NOTES[builtin]} Built-in roles can't be renamed or deleted.</p>
      ) : (
        <Input label="Role name" name="name" placeholder="Triage" autoComplete="off" defaultValue={name} error={errors?.name} />
      )}
      <div className="grid gap-6" role="group" aria-label="Permissions">
        {groups.map((group) => {
          const denied = builtin === 'guest' && GUEST_DENIED_GROUPS.includes(group)
          return (
            <fieldset key={group} className="grid gap-3 m-0 p-0" style={{ border: 0 }} disabled={denied}>
              <legend className="text-label text-primary mb-3 p-0">
                {group}
                {denied ? <span className="text-caption text-secondary"> · Not for guests</span> : null}
              </legend>
              {catalog.filter((p) => p.group === group).map((p) => (
                <Checkbox key={p.key} name="permission" value={p.key} label={p.label} defaultChecked={!denied && permissions.includes(p.key)} />
              ))}
            </fieldset>
          )
        })}
        <FormError>{errors?.permission}</FormError>
      </div>
      <FormError>{error}</FormError>
      <Button type="submit" style={{ justifySelf: 'start' }} disabled={pending}>
        {pending ? 'Saving…' : role ? 'Save changes' : 'Add role'}
      </Button>
    </form>
  )
}
