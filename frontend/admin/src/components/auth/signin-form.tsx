import { useNavigate } from '@tanstack/react-router'
import { useActionState, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Input } from '@taskwell/shared/input'
import { email as checkEmail, required } from '@taskwell/shared/validate'
import { api, toFormErrors, type FieldErrors } from '@/api'

type State = { email: string; errors?: FieldErrors; error?: string | null }

// Password first, then the authenticator code. The factor is set up by an operator with the taskwell-admin CLI.
export function SigninForm({ redirect }: { redirect?: string }) {
  const navigate = useNavigate()
  const [mfa, setMfa] = useState(false)

  const [state, submit, pending] = useActionState(async (_prev: State, form: FormData): Promise<State> => {
    const email = String(form.get('email') ?? '').trim()
    const errors = { email: checkEmail(email), password: required('Password')(String(form.get('password') ?? '')) }
    if (errors.email || errors.password) return { email, errors }
    form.set('email', email)
    try {
      await api('/auth/signin', form)
      setMfa(true)
    } catch (e) {
      return { email, ...toFormErrors(e) }
    }
    return { email }
  }, { email: '' })

  if (mfa) return <MfaForm onBack={() => setMfa(false)} onDone={() => navigate({ href: redirect ?? '/' })} />

  return (
    <form action={submit} noValidate className="grid gap-4">
      <Input
        label="Email"
        name="email"
        type="email"
        autoComplete="email"
        placeholder="you@company.com"
        defaultValue={state.email}
        error={state.errors?.email}
      />
      <Input
        label="Password"
        name="password"
        type="password"
        autoComplete="current-password"
        placeholder="••••••••"
        error={state.errors?.password}
      />
      {state.error ? <p role="alert" className="text-caption text-accent">{state.error}</p> : null}
      <Button type="submit" className="w-full" disabled={pending}>
        {pending ? 'Signing in…' : 'Sign in'}
      </Button>
    </form>
  )
}

type MfaState = { errors?: FieldErrors; error?: string | null }

function MfaForm({ onBack, onDone }: { onBack: () => void; onDone: () => void }) {
  const [useRecovery, setUseRecovery] = useState(false)
  const field = useRecovery ? 'recovery_code' : 'code'
  const [state, submit, pending] = useActionState(async (_prev: MfaState, form: FormData): Promise<MfaState> => {
    const value = String(form.get(field) ?? '').trim()
    const error = useRecovery ? required('Recovery code')(value) : /^\d{6}$/.test(value.replace(/\s/g, '')) ? null : 'Enter the 6-digit code.'
    if (error) return { errors: { [field]: error } }
    try {
      await api('/auth/mfa', form)
      onDone()
    } catch (e) {
      return toFormErrors(e)
    }
    return {}
  }, {})

  return (
    <form action={submit} noValidate className="grid gap-4">
      <p className="text-body-sm text-secondary">
        {useRecovery ? 'Enter one of your recovery codes. Each works once.' : 'Enter the code from your authenticator app.'}
      </p>
      {useRecovery ? (
        <Input key="recovery" label="Recovery code" name="recovery_code" autoComplete="off" placeholder="xxxx-xxxx-xxxx" error={state.errors?.recovery_code} />
      ) : (
        <Input key="code" label="Code" name="code" inputMode="numeric" autoComplete="one-time-code" placeholder="123456" error={state.errors?.code} />
      )}
      {state.error ? <p role="alert" className="text-caption text-accent">{state.error}</p> : null}
      <Button type="submit" className="w-full" disabled={pending}>
        {pending ? 'Checking…' : 'Continue'}
      </Button>
      <div className="flex justify-between">
        <Button variant="ghost" onClick={onBack}>
          Back
        </Button>
        <Button variant="ghost" onClick={() => setUseRecovery(!useRecovery)}>
          {useRecovery ? 'Use the authenticator app' : 'Use a recovery code'}
        </Button>
      </div>
    </form>
  )
}
