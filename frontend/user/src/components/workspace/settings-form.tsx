import { useNavigate, useRouter } from '@tanstack/react-router'
import { useActionState } from 'react'
import { Button } from '@tether/shared/button'
import { Input } from '@tether/shared/input'
import { workspaceName, workspaceSlug } from '@tether/shared/validate'
import { api, toFormErrors, type FieldErrors, type Workspace } from '@/api'
import { FormError } from '@/components/auth/auth-card'

type Values = { name: string; slug: string }
type State = { values: Values; errors?: FieldErrors; error?: string | null; saved?: boolean }

export function WorkspaceSettingsForm({ workspace }: { workspace: Workspace }) {
  const router = useRouter()
  const navigate = useNavigate()
  const readOnly = workspace.role !== 'admin'

  const [{ values, errors, error, saved }, submit, pending] = useActionState(async (_prev: State, form: FormData): Promise<State> => {
    const values = { name: String(form.get('name') ?? '').trim(), slug: String(form.get('slug') ?? '').trim().toLowerCase() }
    const errors = { name: workspaceName(values.name), slug: workspaceSlug(values.slug) }
    if (Object.values(errors).some(Boolean)) return { values, errors }
    form.set('name', values.name)
    form.set('slug', values.slug)
    let updated: Workspace
    try {
      updated = await api<Workspace>(`/workspaces/${encodeURIComponent(workspace.slug)}`, form)
    } catch (e) {
      return { values, ...toFormErrors(e) }
    }
    // A new slug means a new URL; navigating there also reloads the navbar list. Same slug: reload in place.
    if (updated.slug !== workspace.slug) await navigate({ to: '/$workspace/settings/workspace', params: { workspace: updated.slug }, replace: true })
    else await router.invalidate()
    return { values: { name: updated.name, slug: updated.slug }, saved: true }
  }, { values: { name: workspace.name, slug: workspace.slug } })

  return (
    <form action={submit} noValidate className="grid gap-4">
      <Input label="Workspace name" name="name" readOnly={readOnly} defaultValue={values.name} error={errors?.name} />
      <Input
        label="Workspace URL"
        name="slug"
        prefix="tether.app/"
        autoCapitalize="none"
        spellCheck={false}
        readOnly={readOnly}
        defaultValue={values.slug}
        error={errors?.slug}
      />
      {readOnly ? (
        <p className="text-caption text-secondary">Only admins can change workspace settings.</p>
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
