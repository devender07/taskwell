import { useEffect, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Dialog } from '@taskwell/shared/dialog'
import { Input } from '@taskwell/shared/input'
import { required, sixDigitCode } from '@taskwell/shared/validate'
import { getReauthMethods, reauth, sendReauthCode, setConfirmIdentity, toFormErrors, type FieldErrors, type ReauthMethod } from '@/api'
import { FormError } from '@/components/auth/auth-card'

type Ask = { id: number; methods: ReauthMethod[]; resolve: (ok: boolean) => void }
type State = { errors?: FieldErrors; error?: string | null; sent?: boolean }

const DESCRIPTION: Record<ReauthMethod, string> = {
  password: 'Enter your password to continue.',
  totp: 'Enter the 6-digit code from your authenticator app.',
  email: "We'll email you a 6-digit code.",
}
const SWITCH: Record<ReauthMethod, string> = {
  password: 'Use your password instead',
  totp: 'Use your authenticator app instead',
  email: 'Email me a code instead',
}

// Mounted once in the _app layout. api() calls it on a 403 {reauth}; concurrent asks share one dialog.
export function ReauthDialog() {
  const [ask, setAsk] = useState<Ask | null>(null)
  const [open, setOpen] = useState(false)

  useEffect(() => {
    let waiting: Promise<boolean> | null = null
    let id = 0
    setConfirmIdentity(() => {
      waiting ??= getReauthMethods()
        .then(({ methods }) => new Promise<boolean>((resolve) => {
          setAsk({ id: ++id, methods, resolve })
          setOpen(true)
        }))
        .finally(() => (waiting = null))
      return waiting
    })
    return () => setConfirmIdentity(null)
  }, [])

  const finish = (ok: boolean) => {
    ask?.resolve(ok)
    setOpen(false)
  }

  return (
    <Dialog open={open} onClose={() => finish(false)} title="Confirm it's you">
      {ask ? <ReauthForm key={ask.id} methods={ask.methods} onDone={() => finish(true)} /> : null}
    </Dialog>
  )
}

function ReauthForm({ methods, onDone }: { methods: ReauthMethod[]; onDone: () => void }) {
  const [method, setMethod] = useState(methods[0] ?? 'email')
  const field = method === 'password' ? 'password' : method === 'totp' ? 'code' : 'email_code'

  // Not an action: the call that needs confirming is still pending in its own transition, and React holds every
  // transition's updates until all of them settle, so an action here would never show a wrong-password error.
  const [state, setState] = useState<State>({})
  const [pending, setPending] = useState(false)
  const run = async (form: FormData | null) => {
    if (!form) {
      try {
        await sendReauthCode()
        return { sent: true }
      } catch (e) {
        return toFormErrors(e)
      }
    }
    const value = String(form.get(field) ?? '')
    const error = field === 'password' ? required('Password')(value) : sixDigitCode(value)
    if (error) return { ...state, errors: { [field]: error }, error: null }
    if (field !== 'password') form.set(field, value.replace(/\s/g, ''))
    try {
      await reauth(form)
    } catch (e) {
      return { ...state, ...toFormErrors(e) }
    }
    onDone()
    return state
  }
  const submit = async (form: FormData | null) => {
    setPending(true)
    setState(await run(form))
    setPending(false)
  }

  const emailStart = method === 'email' && !state.sent
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        void submit(emailStart ? null : new FormData(e.currentTarget))
      }}
      noValidate
      className="grid gap-4"
    >
      <p className="text-body-sm text-secondary m-0">{method === 'email' && state.sent ? 'Enter the 6-digit code we emailed you.' : DESCRIPTION[method]}</p>
      {method === 'password' ? (
        <Input key="password" label="Password" name="password" type="password" autoComplete="current-password" autoFocus error={state.errors?.password} />
      ) : emailStart ? null : (
        <Input
          key={field}
          label="Code"
          name={field}
          style={{ font: '400 20px/1 var(--mono)', letterSpacing: 8, textAlign: 'center' }}
          inputMode="numeric"
          autoComplete="one-time-code"
          maxLength={7}
          placeholder="000000"
          autoFocus
          error={state.errors?.[field]}
        />
      )}
      <FormError>{state.error}</FormError>
      <Button type="submit" className="w-full" disabled={pending}>
        {emailStart ? (pending ? 'Sending…' : 'Email me a code') : pending ? 'Confirming…' : 'Confirm'}
      </Button>
      {methods.filter((m) => m !== method).map((m) => (
        <Button key={m} variant="ghost" onClick={() => setMethod(m)} disabled={pending}>
          {SWITCH[m]}
        </Button>
      ))}
    </form>
  )
}
