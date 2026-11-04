import { useNavigate } from '@tanstack/react-router'
import { useActionState, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Input } from '@taskwell/shared/input'
import { slugify, workspaceName, workspaceSlug } from '@taskwell/shared/validate'
import { api, toFormErrors, type FieldErrors, type Workspace } from '@/api'
import { FormError } from '@/components/auth/auth-card'

type State = { errors?: FieldErrors; error?: string | null }

export function CreateWorkspaceForm({ onDone }: { onDone?: () => void }) {
  const navigate = useNavigate()
  const [name, setName] = useState('')
  const [editedSlug, setEditedSlug] = useState<string | null>(null) // null until the user types in the URL field
  const slug = editedSlug ?? slugify(name)

  const [{ errors, error }, submit, pending] = useActionState(async (_prev: State, form: FormData): Promise<State> => {
    form.set('name', String(form.get('name') ?? '').trim())
    form.set('slug', String(form.get('slug') ?? '').trim().toLowerCase())
    const errors = { name: workspaceName(String(form.get('name'))), slug: workspaceSlug(String(form.get('slug'))) }
    if (Object.values(errors).some(Boolean)) return { errors }
    let created: Workspace
    try {
      created = await api<Workspace>('/workspaces', form)
    } catch (e) {
      return toFormErrors(e)
    }
    // Navigating re-runs the _app beforeLoad, so the navbar list picks up the new workspace.
    await navigate({ to: '/$workspace', params: { workspace: created.slug } })
    onDone?.()
    return {}
  }, {})

  return (
    <form action={submit} noValidate className="grid gap-4">
      <Input label="Workspace name" name="name" placeholder="Acme" autoFocus value={name} onChange={(e) => setName(e.target.value)} error={errors?.name} />
      <Input
        label="Workspace URL"
        name="slug"
        prefix={`${location.host}/`}
        placeholder="acme"
        autoCapitalize="none"
        spellCheck={false}
        value={slug}
        onChange={(e) => setEditedSlug(e.target.value)}
        error={errors?.slug}
      />
      <FormError>{error}</FormError>
      <Button type="submit" className="w-full" disabled={pending}>
        {pending ? 'Creating workspace…' : 'Create workspace'}
      </Button>
    </form>
  )
}
