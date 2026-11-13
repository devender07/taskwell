import { Link, useNavigate } from '@tanstack/react-router'
import { useActionState, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Input } from '@taskwell/shared/input'
import { recoveryCode, sixDigitCode } from '@taskwell/shared/validate'
import { ApiError, api, homePath, toFormErrors, type FieldErrors } from '@/api'
import { FormError } from './auth-card'

type State = { errors?: FieldErrors; error?: string | null; expired?: boolean }

// The second step of a sign-in: the pending sign-in lives in an HttpOnly cookie, so this posts just the code.
export function MfaForm({ redirect }: { redirect?: string }) {
  const navigate = useNavigate()
  const [recovery, setRecovery] = useState(false)
  const [state, submit, pending] = useActionState(async (_prev: State, form: FormData): Promise<State> => {
    const field = recovery ? 'recovery_code' : 'code'
    const value = String(form.get(field) ?? '').trim()
    const fieldError = recovery ? recoveryCode(value) : sixDigitCode(value)
    if (fieldError) return { errors: { [field]: fieldError } }
    if (!recovery) form.set('code', value.replace(/\s/g, ''))
    try {
      await api('/auth/mfa', form)
    } catch (e) {
      const { errors, error } = toFormErrors(e)
      return { errors, error, expired: e instanceof ApiError && e.status === 401 }
    }
    await navigate({ href: redirect ?? (await homePath()) })
    return {}
  }, {})

  if (state.expired) {
    return (
      <div className="grid gap-4">
        <FormError>{state.error ?? 'Your sign-in timed out. Start again.'}</FormError>
        <Link to="/signin" search={redirect ? { redirect } : {}} className="button button-primary w-full">
          Back to sign in
        </Link>
      </div>
    )
  }

  return (
    <form action={submit} noValidate className="grid gap-4">
      {recovery ? (
        <Input
          key="recovery"
          label="Recovery code"
          name="recovery_code"
          autoComplete="off"
          autoCapitalize="none"
          spellCheck={false}
          placeholder="xxxx-xxxx-xxxx"
          style={{ fontFamily: 'var(--mono)' }}
          autoFocus
          error={state.errors?.recovery_code}
        />
      ) : (
        <Input
          key="code"
          label="Code"
          name="code"
          style={{ font: '400 20px/1 var(--mono)', letterSpacing: 8, textAlign: 'center' }}
          inputMode="numeric"
          autoComplete="one-time-code"
          maxLength={7}
          placeholder="000000"
          autoFocus
          error={state.errors?.code}
        />
      )}
      <FormError>{state.error}</FormError>
      <Button type="submit" className="w-full" disabled={pending}>
        {pending ? 'Verifying…' : 'Verify'}
      </Button>
      <Button variant="ghost" onClick={() => setRecovery(!recovery)} disabled={pending}>
        {recovery ? 'Use a code from your app instead' : 'Use a recovery code instead'}
      </Button>
    </form>
  )
}
