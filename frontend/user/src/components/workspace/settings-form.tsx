import { useNavigate, useRouter } from '@tanstack/react-router'
import { useActionState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Checkbox } from '@taskwell/shared/checkbox'
import { Input } from '@taskwell/shared/input'
import { workspaceName, workspaceSlug } from '@taskwell/shared/validate'
import { api, can, toFormErrors, type FieldErrors, type Workspace, workspacePath } from '@/api'
import { FormError } from '@/components/auth/auth-card'

type Values = { name: string; slug: string; require_mfa: boolean }
type State = { values: Values; errors?: FieldErrors; error?: string | null; saved?: boolean }

export function WorkspaceSettingsForm({ workspace }: { workspace: Workspace }) {
  const router = useRouter()
  const navigate = useNavigate()
  const readOnly = !can(workspace, 'workspace.edit')

  const [{ values, errors, error, saved }, submit, pending] = useActionState(async (_prev: State, form: FormData): Promise<State> => {
    const values = { name: String(form.get('name') ?? '').trim(), slug: String(form.get('slug') ?? '').trim().toLowerCase(), require_mfa: form.has('require_mfa') }
    const errors = { name: workspaceName(values.name), slug: workspaceSlug(values.slug) }
    if (Object.values(errors).some(Boolean)) return { values, errors }
    form.set('name', values.name)
    form.set('slug', values.slug)
    form.set('require_mfa', String(values.require_mfa))
    let updated: Workspace
    try {
      updated = await api<Workspace>(`${workspacePath(workspace.slug)}`, form)
    } catch (e) {
      return { values, ...toFormErrors(e) }
    }
    // A new slug means a new URL; navigating there also reloads the navbar list. Same slug: reload in place.
    if (updated.slug !== workspace.slug) await navigate({ to: '/$workspace/settings/workspace', params: { workspace: updated.slug }, replace: true })
    else await router.invalidate()
    return { values: { name: updated.name, slug: updated.slug, require_mfa: updated.require_mfa ?? values.require_mfa }, saved: true }
  }, { values: { name: workspace.name, slug: workspace.slug, require_mfa: workspace.require_mfa ?? false } })

  return (
    <form action={submit} noValidate className="grid gap-4">
      <Input label="Workspace name" name="name" readOnly={readOnly} defaultValue={values.name} error={errors?.name} />
      <Input
        label="Workspace URL"
        name="slug"
        prefix={`${location.host}/`}
        autoCapitalize="none"
        spellCheck={false}
        readOnly={readOnly}
        defaultValue={values.slug}
        error={errors?.slug}
      />
      {readOnly ? null : (
        <div className="grid gap-1">
          <Checkbox key={String(values.require_mfa)} name="require_mfa" label="Require two-factor sign-in" defaultChecked={values.require_mfa} />
          <p className="text-caption text-secondary">Members without it can't open the workspace until they turn it on.</p>
          <FormError>{errors?.require_mfa}</FormError>
        </div>
      )}
      {readOnly ? (
        <p className="text-caption text-secondary">You don't have permission to change workspace settings.</p>
      ) : (
        <>
          <FormError>{error}</FormError>
          <div className="flex items-center gap-3">
            <Button type="submit" disabled={pending}>
              {pending ? 'Saving…' : 'Save changes'}
            </Button>
            {saved && !pending ? <p role="status" className="text-caption text-success">Saved.</p> : null}
          </div>
        </>
      )}
    </form>
  )
}
