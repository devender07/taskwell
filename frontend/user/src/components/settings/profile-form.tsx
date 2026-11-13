import { useRouter } from '@tanstack/react-router'
import { useActionState } from 'react'
import { Avatar } from '@taskwell/shared/avatar'
import { Button } from '@taskwell/shared/button'
import { Input } from '@taskwell/shared/input'
import { name as checkName } from '@taskwell/shared/validate'
import { api, toFormErrors, type FieldErrors, type User } from '@/api'
import { FormError } from '@/components/auth/auth-card'

type State = { name: string; errors?: FieldErrors; error?: string | null; saved?: boolean }

export function ProfileForm({ user }: { user: User }) {
  const router = useRouter()
  const [{ name, errors, error, saved }, submit, pending] = useActionState(async (_prev: State, form: FormData): Promise<State> => {
    const name = String(form.get('name') ?? '').trim()
    const nameError = checkName(name)
    if (nameError) return { name, errors: { name: nameError } }
    form.set('name', name)
    try {
      await api<User>('/me', form)
    } catch (e) {
      return { name, ...toFormErrors(e) }
    }
    await router.invalidate() // re-runs the _app guard, so the navbar avatar and greeting pick up the new name
    return { name, saved: true }
  }, { name: user.name })

  return (
    <form action={submit} noValidate className="grid gap-4">
      <div className="flex items-center gap-3">
        <Avatar name={name || user.name} size={48} src={user.avatar_url} />
        <div className="grid">
          <span className="text-label text-primary">{user.name}</span>
          <span className="text-caption text-secondary">{user.email}</span>
        </div>
      </div>
      <Input label="Full name" name="name" autoComplete="name" defaultValue={name} error={errors?.name} />
      <FormError>{error}</FormError>
      <div className="flex items-center gap-3">
        <Button type="submit" disabled={pending}>
          {pending ? 'Saving…' : 'Save changes'}
        </Button>
        {saved && !pending ? <p role="status" className="text-caption text-success">Saved.</p> : null}
      </div>
    </form>
  )
}
