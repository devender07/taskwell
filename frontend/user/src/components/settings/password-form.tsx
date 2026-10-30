import { useRouter } from '@tanstack/react-router'
import { useActionState } from 'react'
import { Button } from '@tether/shared/button'
import { Input } from '@tether/shared/input'
import { password as checkPassword, required } from '@tether/shared/validate'
import { api, toFormErrors, type FieldErrors, type User } from '@/api'
import { FormError } from '@/components/auth/auth-card'

type State = { errors?: FieldErrors; error?: string | null; saved?: boolean }

export function PasswordForm({ user }: { user: User }) {
  const router = useRouter()
  const [{ errors, error, saved }, submit, pending] = useActionState(async (_prev: State, form: FormData): Promise<State> => {
    const errors = {
      current_password: user.has_password ? required('Current password')(String(form.get('current_password') ?? '')) : null,
      password: checkPassword(String(form.get('password') ?? '')),
    }
    if (errors.current_password || errors.password) return { errors }
    try {
      await api('/auth/password/change', form)
    } catch (e) {
      return toFormErrors(e)
    }
    if (!user.has_password) await router.invalidate()
    return { saved: true }
  }, {})

  return (
    <form action={submit} noValidate className="grid gap-4">
      <div className="grid gap-1">
        <h2 className="text-label">{user.has_password ? 'Change password' : 'Set a password'}</h2>
        <p className="text-body-sm text-secondary">
          {user.has_password
            ? "You'll stay signed in here; other devices are signed out."
            : 'You sign in with Google or an email code. Add a password to sign in with it too.'}
        </p>
      </div>
      <input type="text" name="email" autoComplete="username" value={user.email} readOnly hidden />
      {user.has_password ? (
        <Input label="Current password" name="current_password" type="password" autoComplete="current-password" error={errors?.current_password} />
      ) : null}
      <Input label="New password" name="password" type="password" autoComplete="new-password" error={errors?.password} />
      <FormError>{error}</FormError>
      <div className="flex items-center gap-3">
        <Button type="submit" disabled={pending}>
          {pending ? 'Saving…' : user.has_password ? 'Change password' : 'Set password'}
        </Button>
        {saved && !pending ? <p role="status" className="text-caption text-success">Password updated.</p> : null}
      </div>
    </form>
  )
}
