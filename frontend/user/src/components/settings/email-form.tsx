import { useRouter } from '@tanstack/react-router'
import { useActionState, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Input } from '@taskwell/shared/input'
import { email as checkEmail } from '@taskwell/shared/validate'
import { api, toFormErrors, type FieldErrors, type User } from '@/api'
import { FormError } from '@/components/auth/auth-card'

type State = { errors?: FieldErrors; error?: string | null }

const checkCode = (v: string) => (/^\d{6}$/.test(v.trim()) ? null : 'Enter the 6-digit code.')

// Changing the email takes a code from the current address and one from the new address.
export function EmailForm({ user }: { user: User }) {
  const router = useRouter()
  const [step, setStep] = useState<'idle' | 'address' | 'codes'>('idle')
  const [to, setTo] = useState('')
  const [changed, setChanged] = useState(false)

  const [sent, send, sending] = useActionState(async (): Promise<State> => {
    const err = checkEmail(to)
    if (err) return { errors: { email: err } }
    const form = new FormData()
    form.set('email', to.trim())
    try {
      await api('/auth/email/code', form)
    } catch (e) {
      return toFormErrors(e)
    }
    setStep('codes')
    return {}
  }, {})

  const [confirmed, confirm, confirming] = useActionState(async (_prev: State, form: FormData): Promise<State> => {
    const errors = { old_code: checkCode(String(form.get('old_code') ?? '')), new_code: checkCode(String(form.get('new_code') ?? '')) }
    if (errors.old_code || errors.new_code) return { errors }
    try {
      await api<User>('/auth/email/change', form)
    } catch (e) {
      return toFormErrors(e)
    }
    await router.invalidate() // the _app guard refetches /me, so everything shows the new address
    setStep('idle')
    setTo('')
    setChanged(true)
    return {}
  }, {})

  const cancel = () => {
    setStep('idle')
    setTo('')
  }

  return (
    <div className="grid gap-4">
      <div className="grid gap-1">
        <h2 className="text-label">Email</h2>
        <p className="text-body-sm text-secondary">
          {step === 'codes'
            ? `We sent a code to ${user.email} and one to ${to.trim()}. Enter both to switch.`
            : "You sign in and get notifications here. To change it, you'll confirm with a code sent to both addresses."}
        </p>
      </div>
      {step === 'idle' ? (
        <div className="flex flex-wrap items-center gap-3">
          <span className="text-body-sm text-primary flex-grow">{user.email}</span>
          {changed ? <p role="status" className="text-caption text-success">Email changed.</p> : null}
          <Button variant="secondary" onClick={() => (setStep('address'), setChanged(false))}>
            Change email
          </Button>
        </div>
      ) : step === 'address' ? (
        <form action={send} noValidate className="grid gap-4">
          <Input
            label="New email"
            type="email"
            autoComplete="email"
            autoFocus
            placeholder="you@company.com"
            value={to}
            onChange={(e) => setTo(e.target.value)}
            error={sent.errors?.email}
          />
          <FormError>{sent.error}</FormError>
          <div className="flex items-center gap-3">
            <Button type="submit" disabled={sending}>
              {sending ? 'Sending…' : 'Send codes'}
            </Button>
            <Button variant="ghost" onClick={cancel} disabled={sending}>
              Cancel
            </Button>
          </div>
        </form>
      ) : (
        <form action={confirm} noValidate className="grid gap-4">
          <div className="grid gap-4" style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))' }}>
            <Input
              label={`Code from ${user.email}`}
              name="old_code"
              inputMode="numeric"
              autoComplete="one-time-code"
              placeholder="123456"
              autoFocus
              error={confirmed.errors?.old_code}
            />
            <Input label={`Code from ${to.trim()}`} name="new_code" inputMode="numeric" autoComplete="off" placeholder="123456" error={confirmed.errors?.new_code} />
          </div>
          <FormError>{confirmed.errors?.email ?? confirmed.error ?? sent.error}</FormError>
          <div className="flex flex-wrap items-center gap-3">
            <Button type="submit" disabled={confirming}>
              {confirming ? 'Changing…' : 'Change email'}
            </Button>
            <Button variant="ghost" onClick={() => setStep('address')} disabled={confirming}>
              Send new codes
            </Button>
            <Button variant="ghost" onClick={cancel} disabled={confirming}>
              Cancel
            </Button>
          </div>
        </form>
      )}
    </div>
  )
}
