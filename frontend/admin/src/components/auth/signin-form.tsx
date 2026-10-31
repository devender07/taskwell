import { useNavigate } from '@tanstack/react-router'
import { useActionState, useState } from 'react'
import { Button } from '@tether/shared/button'
import { Input } from '@tether/shared/input'
import { email as checkEmail, required } from '@tether/shared/validate'
import { api, toFormErrors, type FieldErrors, type MfaChallenge, type MfaResult } from '@/api'

const mono = { fontFamily: 'var(--mono)' }

type State = { email: string; errors?: FieldErrors; error?: string | null }

// Password first, then the authenticator code (setting it up on the first sign-in), then, once, the recovery codes.
export function SigninForm({ redirect }: { redirect?: string }) {
  const navigate = useNavigate()
  const [challenge, setChallenge] = useState<MfaChallenge | null>(null)
  const [recovery, setRecovery] = useState<string[] | null>(null)
  const done = () => navigate({ href: redirect ?? '/' })

  const [state, submit, pending] = useActionState(async (_prev: State, form: FormData): Promise<State> => {
    const email = String(form.get('email') ?? '').trim()
    const errors = { email: checkEmail(email), password: required('Password')(String(form.get('password') ?? '')) }
    if (errors.email || errors.password) return { email, errors }
    form.set('email', email)
    try {
      setChallenge(await api<MfaChallenge>('/auth/signin', form))
    } catch (e) {
      return { email, ...toFormErrors(e) }
    }
    return { email }
  }, { email: '' })

  if (recovery) return <RecoveryCodes codes={recovery} onDone={done} />
  if (challenge)
    return (
      <MfaForm
        challenge={challenge}
        onBack={() => setChallenge(null)}
        onDone={(r) => (r.recovery_codes?.length ? setRecovery(r.recovery_codes) : done())}
      />
    )

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

function MfaForm({ challenge, onBack, onDone }: { challenge: MfaChallenge; onBack: () => void; onDone: (r: MfaResult) => void }) {
  const [useRecovery, setUseRecovery] = useState(false)
  const field = useRecovery ? 'recovery_code' : 'code'
  const [state, submit, pending] = useActionState(async (_prev: MfaState, form: FormData): Promise<MfaState> => {
    const value = String(form.get(field) ?? '').trim()
    const error = useRecovery ? required('Recovery code')(value) : /^\d{6}$/.test(value.replace(/\s/g, '')) ? null : 'Enter the 6-digit code.'
    if (error) return { errors: { [field]: error } }
    try {
      onDone(await api<MfaResult>('/auth/mfa', form))
    } catch (e) {
      return toFormErrors(e)
    }
    return {}
  }, {})

  return (
    <form action={submit} noValidate className="grid gap-4">
      {challenge.mfa === 'enrol' ? (
        <div className="grid gap-2 text-body-sm">
          <p className="text-secondary">
            Set up two-step sign-in. In your authenticator app, add an account with this setup key (time-based, 6 digits), then enter the code it shows.
          </p>
          <code className="p-3 border rounded" style={{ ...mono, wordBreak: 'break-all', userSelect: 'all' }}>{challenge.secret}</code>
          <details>
            <summary className="text-caption text-secondary">Setup link (otpauth://)</summary>
            <code className="text-caption" style={{ ...mono, wordBreak: 'break-all', userSelect: 'all' }}>{challenge.otpauth_url}</code>
          </details>
        </div>
      ) : (
        <p className="text-body-sm text-secondary">
          {useRecovery ? 'Enter one of your recovery codes. Each works once.' : 'Enter the code from your authenticator app.'}
        </p>
      )}
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
        {challenge.mfa === 'verify' ? (
          <Button variant="ghost" onClick={() => setUseRecovery(!useRecovery)}>
            {useRecovery ? 'Use the authenticator app' : 'Use a recovery code'}
          </Button>
        ) : null}
      </div>
    </form>
  )
}

function RecoveryCodes({ codes, onDone }: { codes: string[]; onDone: () => void }) {
  return (
    <div className="grid gap-4">
      <p className="text-body-sm text-secondary">
        Save these recovery codes somewhere safe. Each signs you in once if you lose your authenticator app. They won't be shown again.
      </p>
      <pre className="m-0 p-3 border rounded text-body-sm" style={{ ...mono, userSelect: 'all' }}>{codes.join('\n')}</pre>
      <Button type="button" className="w-full" onClick={onDone}>
        I've saved them
      </Button>
    </div>
  )
}
