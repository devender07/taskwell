import { useRouter } from '@tanstack/react-router'
import { useActionState, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Checkbox } from '@taskwell/shared/checkbox'
import { Input } from '@taskwell/shared/input'
import { recoveryCode, required, sixDigitCode } from '@taskwell/shared/validate'
import { api, mfaPath, toFormErrors, type FieldErrors, type MfaStatus, type User } from '@/api'
import { FormError } from '@/components/auth/auth-card'
import { reloadPage } from '@/realtime'

type Step =
  | { name: 'idle' }
  | { name: 'confirm' }
  | { name: 'secret'; secret: string; url: string }
  | { name: 'codes'; codes: string[] }
  | { name: 'new-codes' }
  | { name: 'disable' }

type State = { errors?: FieldErrors; error?: string | null }

const mono = { fontFamily: 'var(--mono)', overflowWrap: 'anywhere' } as const

// Turning it on: confirm it's you, add the secret to an authenticator app, prove it with a code, save the recovery codes.
export function TwoFactor({ user, status }: { user: User; status: MfaStatus }) {
  const router = useRouter()
  const [step, setStep] = useState<Step>({ name: 'idle' })
  const cancel = () => setStep({ name: 'idle' })
  const finish = async () => {
    await reloadPage(router)
    setStep({ name: 'idle' })
  }

  return (
    <div className="grid gap-4">
      <div className="grid gap-1">
        <h2 className="text-label">Two-factor sign-in</h2>
        <p className="text-body-sm text-secondary">
          {status.enabled
            ? 'On. Signing in also asks for a code from your authenticator app.'
            : 'Ask for a code from an authenticator app every time you sign in, whichever way you sign in.'}
        </p>
      </div>
      {step.name === 'confirm' ? (
        <ConfirmStep user={user} onDone={(secret, url) => setStep({ name: 'secret', secret, url })} onCancel={cancel} />
      ) : step.name === 'secret' ? (
        <SecretStep secret={step.secret} url={step.url} onDone={(codes) => setStep({ name: 'codes', codes })} onCancel={cancel} />
      ) : step.name === 'codes' ? (
        <CodesStep codes={step.codes} onDone={finish} />
      ) : step.name === 'new-codes' ? (
        <CodeStep
          path={`${mfaPath}/recovery`}
          hint="Your old recovery codes stop working."
          submit={['New recovery codes', 'Creating…']}
          onDone={(res) => setStep({ name: 'codes', codes: (res as { recovery_codes: string[] }).recovery_codes })}
          onCancel={cancel}
        />
      ) : step.name === 'disable' ? (
        <CodeStep path={`${mfaPath}/disable`} recovery hint="Your other devices are signed out." submit={['Turn off', 'Turning off…']} danger onDone={finish} onCancel={cancel} />
      ) : status.enabled ? (
        <div className="flex flex-wrap items-center gap-3">
          <p className="text-body-sm text-secondary flex-grow m-0">
            {status.recovery_left} recovery {status.recovery_left === 1 ? 'code' : 'codes'} left
          </p>
          <Button variant="secondary" onClick={() => setStep({ name: 'new-codes' })}>
            New recovery codes
          </Button>
          <Button variant="ghost" style={{ color: 'var(--accent)' }} onClick={() => setStep({ name: 'disable' })}>
            Turn off
          </Button>
        </div>
      ) : (
        <Button style={{ justifySelf: 'start' }} onClick={() => setStep({ name: 'confirm' })}>
          Turn on
        </Button>
      )}
    </div>
  )
}

// Like changing the password: the current password, or an emailed code for accounts without one.
function ConfirmStep({ user, onDone, onCancel }: { user: User; onDone: (secret: string, url: string) => void; onCancel: () => void }) {
  const [code, setCode] = useState<{ sent?: boolean; sending?: boolean; error?: string | null }>({})
  const [{ errors, error }, submit, pending] = useActionState(async (_prev: State, form: FormData): Promise<State> => {
    const errors = user.has_password
      ? { password: required('Password')(String(form.get('password') ?? '')) }
      : { email_code: sixDigitCode(String(form.get('email_code') ?? '')) }
    if (Object.values(errors).some(Boolean)) return { errors }
    try {
      const res = await api<{ secret: string; otpauth_url: string }>(`${mfaPath}/setup`, form)
      onDone(res.secret, res.otpauth_url)
    } catch (e) {
      return toFormErrors(e)
    }
    return {}
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
      <input type="text" name="email" autoComplete="username" value={user.email} readOnly hidden />
      {user.has_password ? (
        <Input label="Password" name="password" type="password" autoComplete="current-password" autoFocus error={errors?.password} />
      ) : (
        <div className="grid gap-2">
          <p className="text-body-sm text-secondary m-0">We'll email you a code to confirm it's you.</p>
          <div className="flex flex-wrap items-center gap-3">
            <Button type="button" variant="secondary" onClick={sendCode} disabled={code.sending}>
              {code.sending ? 'Sending…' : code.sent ? 'Send a new code' : 'Email me a code'}
            </Button>
            {code.sent ? <p role="status" className="text-caption text-secondary">Code sent to {user.email}.</p> : null}
          </div>
          <FormError>{code.error}</FormError>
          <Input label="Code" name="email_code" inputMode="numeric" autoComplete="one-time-code" placeholder="123456" error={errors?.email_code} />
        </div>
      )}
      <FormError>{error}</FormError>
      <Actions pending={pending} submit={['Continue', 'Checking…']} onCancel={onCancel} />
    </form>
  )
}

function SecretStep({ secret, url, onDone, onCancel }: { secret: string; url: string; onDone: (codes: string[]) => void; onCancel: () => void }) {
  const [copied, setCopied] = useState<string | null>(null)
  const [{ errors, error }, submit, pending] = useActionState(async (_prev: State, form: FormData): Promise<State> => {
    const value = String(form.get('code') ?? '')
    const fieldError = sixDigitCode(value)
    if (fieldError) return { errors: { code: fieldError } }
    form.set('code', value.replace(/\s/g, ''))
    try {
      const res = await api<{ recovery_codes: string[] }>(`${mfaPath}/enable`, form)
      onDone(res.recovery_codes)
    } catch (e) {
      return toFormErrors(e)
    }
    return {}
  }, {})

  return (
    <form action={submit} noValidate className="grid gap-4">
      <ol className="grid gap-4 m-0 text-body-sm" style={{ paddingLeft: 20 }}>
        <li className="grid gap-2">
          <span>Add Taskwell to your authenticator app with this key, or open the link on this device.</span>
          <div className="flex flex-wrap items-center gap-3">
            <code className="p-3 border rounded flex-grow" style={{ ...mono, minWidth: 0, background: 'rgba(255, 255, 255, 0.02)' }}>
              {secret}
            </code>
            <Button
              variant="secondary"
              onClick={() => navigator.clipboard.writeText(secret).then(() => setCopied('Key copied.'), () => setCopied("Couldn't copy. Select the key instead."))}
            >
              Copy
            </Button>
          </div>
          {copied ? <p role="status" className="text-caption text-secondary m-0">{copied}</p> : null}
          <a href={url} className="auth-link" style={{ justifySelf: 'start' }}>
            Open in authenticator app
          </a>
        </li>
        <li className="grid gap-2">
          <span>Enter the 6-digit code it shows.</span>
          <Input aria-label="Code" name="code" inputMode="numeric" autoComplete="one-time-code" maxLength={7} placeholder="123456" error={errors?.code} />
        </li>
      </ol>
      <FormError>{error}</FormError>
      <Actions pending={pending} submit={['Turn on', 'Checking…']} onCancel={onCancel} />
    </form>
  )
}

function CodesStep({ codes, onDone }: { codes: string[]; onDone: () => void }) {
  const [saved, setSaved] = useState(false)
  const [note, setNote] = useState<string | null>(null)
  const text = codes.join('\n') + '\n'

  function download() {
    const a = document.createElement('a')
    a.href = URL.createObjectURL(new Blob([text], { type: 'text/plain' }))
    a.download = 'taskwell-recovery-codes.txt'
    a.click()
    URL.revokeObjectURL(a.href)
  }

  return (
    <div className="grid gap-4">
      <p className="text-body-sm text-secondary m-0">
        Each recovery code signs you in once if you lose your authenticator app. Keep them somewhere safe; you won't see them again.
      </p>
      <ul className="grid gap-2 m-0 p-4 border rounded" style={{ listStyle: 'none', gridTemplateColumns: 'repeat(auto-fill, minmax(160px, 1fr))', background: 'rgba(255, 255, 255, 0.02)' }}>
        {codes.map((c) => (
          <li key={c} className="text-body-sm" style={mono}>
            {c}
          </li>
        ))}
      </ul>
      <div className="flex flex-wrap items-center gap-3">
        <Button variant="secondary" onClick={() => navigator.clipboard.writeText(text).then(() => setNote('Codes copied.'), () => setNote("Couldn't copy. Download them instead."))}>
          Copy
        </Button>
        <Button variant="secondary" onClick={download}>
          Download .txt
        </Button>
        {note ? <p role="status" className="text-caption text-secondary m-0">{note}</p> : null}
      </div>
      <Checkbox label="I saved these codes" checked={saved} onChange={(e) => setSaved(e.target.checked)} />
      <Button style={{ justifySelf: 'start' }} disabled={!saved} onClick={onDone}>
        Done
      </Button>
    </div>
  )
}

// Asks for a code from the app (or, with `recovery`, a recovery code instead) and posts it to `path`.
function CodeStep({
  path,
  recovery,
  hint,
  submit: labels,
  danger,
  onDone,
  onCancel,
}: {
  path: string
  recovery?: boolean
  hint: string
  submit: [string, string]
  danger?: boolean
  onDone: (res: unknown) => void
  onCancel: () => void
}) {
  const [useRecovery, setUseRecovery] = useState(false)
  const [{ errors, error }, submit, pending] = useActionState(async (_prev: State, form: FormData): Promise<State> => {
    const field = useRecovery ? 'recovery_code' : 'code'
    const value = String(form.get(field) ?? '').trim()
    const fieldError = useRecovery ? recoveryCode(value) : sixDigitCode(value)
    if (fieldError) return { errors: { [field]: fieldError } }
    if (!useRecovery) form.set('code', value.replace(/\s/g, ''))
    try {
      onDone(await api<unknown>(path, form))
    } catch (e) {
      return toFormErrors(e)
    }
    return {}
  }, {})

  return (
    <form action={submit} noValidate className="grid gap-4">
      <p className="text-body-sm text-secondary m-0">{hint}</p>
      {useRecovery ? (
        <Input key="recovery" label="Recovery code" name="recovery_code" autoComplete="off" spellCheck={false} placeholder="xxxx-xxxx-xxxx" autoFocus error={errors?.recovery_code} />
      ) : (
        <Input key="code" label="Code from your app" name="code" inputMode="numeric" autoComplete="one-time-code" maxLength={7} placeholder="123456" autoFocus error={errors?.code} />
      )}
      {recovery ? (
        <Button variant="ghost" style={{ justifySelf: 'start' }} onClick={() => setUseRecovery(!useRecovery)}>
          {useRecovery ? 'Use a code from your app instead' : 'Use a recovery code instead'}
        </Button>
      ) : null}
      <FormError>{error}</FormError>
      <Actions pending={pending} submit={labels} danger={danger} onCancel={onCancel} />
    </form>
  )
}

function Actions({ pending, submit, danger, onCancel }: { pending: boolean; submit: [string, string]; danger?: boolean; onCancel: () => void }) {
  return (
    <div className="flex flex-wrap items-center gap-3">
      <Button type="submit" variant={danger ? 'danger' : 'primary'} disabled={pending}>
        {pending ? submit[1] : submit[0]}
      </Button>
      <Button variant="ghost" onClick={onCancel} disabled={pending}>
        Cancel
      </Button>
    </div>
  )
}
