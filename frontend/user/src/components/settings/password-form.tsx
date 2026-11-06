import { useRouter } from '@tanstack/react-router'
import { useActionState, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Input } from '@taskwell/shared/input'
import { password as checkPassword, required, sixDigitCode } from '@taskwell/shared/validate'
import { api, toFormErrors, type FieldErrors, type User } from '@/api'
import { FormError } from '@/components/auth/auth-card'
import { askToReviewTokens } from './review-tokens'

type State = { errors?: FieldErrors; error?: string | null; saved?: boolean }


export function PasswordForm({ user }: { user: User }) {
  const router = useRouter()
  const [code, setCode] = useState<{ sent?: boolean; sending?: boolean; error?: string | null }>({})
  const [{ errors, error, saved }, submit, pending] = useActionState(async (_prev: State, form: FormData): Promise<State> => {
    const errors = {
      current_password: user.has_password ? required('Current password')(String(form.get('current_password') ?? '')) : null,
      code: user.has_password ? null : sixDigitCode(String(form.get('code') ?? '')),
      password: checkPassword(String(form.get('password') ?? '')),
    }
    if (errors.current_password || errors.code || errors.password) return { errors }
    try {
      await api('/auth/password/change', form)
    } catch (e) {
      return toFormErrors(e)
    }
    if (!user.has_password) await router.invalidate()
    if (user.has_password) askToReviewTokens()
    return { saved: true }
  }, {})

  async function sendCode() {
    setCode({ sending: true })
    try {
      await api('/me/password/code', new FormData())
      setCode({ sent: true })
    } catch (e) {
      setCode({ error: toFormErrors(e).error })
    }
  }

  return (
    <form action={submit} noValidate className="grid gap-4">
      <div className="grid gap-1">
        <h2 className="text-label">{user.has_password ? 'Change password' : 'Set a password'}</h2>
        <p className="text-body-sm text-secondary">
          {user.has_password
            ? "You'll stay signed in here; other devices are signed out."
            : "You sign in with Google or an email code. Add a password to sign in with it too. We'll email you a code to confirm it's you."}
        </p>
      </div>
      <input type="text" name="email" autoComplete="username" value={user.email} readOnly hidden />
      {user.has_password ? (
        <Input label="Current password" name="current_password" type="password" autoComplete="current-password" error={errors?.current_password} />
      ) : (
        <div className="grid gap-2">
          <div className="flex items-center gap-3">
            <Button type="button" variant="secondary" onClick={sendCode} disabled={code.sending}>
              {code.sending ? 'Sending…' : code.sent ? 'Send a new code' : 'Email me a code'}
            </Button>
            {code.sent ? <p role="status" className="text-caption text-secondary">Code sent to {user.email}.</p> : null}
          </div>
          <FormError>{code.error}</FormError>
          <Input label="Code" name="code" inputMode="numeric" autoComplete="one-time-code" placeholder="123456" error={errors?.code} />
        </div>
      )}
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
