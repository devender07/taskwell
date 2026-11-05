import { useNavigate } from '@tanstack/react-router'
import { useActionState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Checkbox } from '@taskwell/shared/checkbox'
import { Input } from '@taskwell/shared/input'
import { email as checkEmail } from '@taskwell/shared/validate'
import { api, homePath, toFormErrors, type FieldErrors } from '@/api'
import { FormError } from './auth-card'

type OtpState = { step: 'email' | 'code'; email: string; resent?: boolean; remember?: boolean; errors?: FieldErrors; error?: string | null; done?: boolean; mfa?: boolean }

// The code step carries the email in a hidden input, so every post is just the form's own FormData.
async function otpAction(prev: OtpState, form: FormData): Promise<OtpState> {
  const intent = form.get('intent')
  if (intent === 'change') return { step: 'email', email: prev.email }
  if (prev.step === 'email' || intent === 'resend') {
    const email = String(form.get('email') ?? '').trim()
    const fieldError = checkEmail(email)
    if (fieldError) return { step: 'email', email, errors: { email: fieldError } }
    try {
      await api('/auth/otp/send', form)
    } catch (e) {
      return { ...prev, email, ...toFormErrors(e) }
    }
    return { step: 'code', email, resent: intent === 'resend' }
  }
  const code = String(form.get('code') ?? '').trim()
  const remember = form.has('remember')
  if (!/^\d{6}$/.test(code)) return { ...prev, remember, errors: { code: 'Enter the 6-digit code.' }, error: null }
  let res: { mfa_required?: boolean } | undefined
  try {
    res = await api<{ mfa_required?: boolean }>('/auth/otp/verify', form)
  } catch (e) {
    const { errors, error } = toFormErrors(e)
    return { ...prev, remember, errors, error: error ?? errors.email }
  }
  return { ...prev, done: true, mfa: res?.mfa_required }
}

export function OtpForm({ redirect }: { redirect?: string }) {
  const navigate = useNavigate()
  const [state, submit, pending] = useActionState(async (prev: OtpState, form: FormData) => {
    const next = await otpAction(prev, form)
    if (next.mfa) await navigate({ to: '/signin/2fa', search: redirect ? { redirect } : {} })
    else if (next.done) await navigate({ href: redirect ?? (await homePath()) })
    return next
  }, { step: 'email', email: '' })

  if (state.step === 'email') {
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
        <FormError>{state.error}</FormError>
        <Button type="submit" className="w-full" disabled={pending}>
          {pending ? 'Sending…' : 'Send code'}
        </Button>
      </form>
    )
  }

  return (
    <form action={submit} noValidate className="grid gap-4">
      <p role="status" className="text-body-sm text-secondary">
        We sent a {state.resent ? 'new ' : ''}6-digit code to <strong className="text-primary font-510">{state.email}</strong>
      </p>
      <input type="hidden" name="email" value={state.email} />
      <Input
        label="Code"
        name="code"
        style={{ font: '400 20px/1 var(--mono)', letterSpacing: 8, textAlign: 'center' }}
        inputMode="numeric"
        autoComplete="one-time-code"
        maxLength={6}
        placeholder="000000"
        autoFocus
        error={state.errors?.code}
      />
      <Checkbox name="remember" label="Remember me" defaultChecked={state.remember} />
      <FormError>{state.error}</FormError>
      {/* ponytail: one pending flag for all three buttons, so a resend also reads "Verifying…"; track the submitter if that confuses anyone. */}
      <Button type="submit" className="w-full" disabled={pending}>
        {pending ? 'Verifying…' : 'Verify and sign in'}
      </Button>
      <div className="flex justify-between">
        <Button variant="ghost" type="submit" name="intent" value="change" disabled={pending}>
          Change email
        </Button>
        <Button variant="ghost" type="submit" name="intent" value="resend" disabled={pending}>
          Resend code
        </Button>
      </div>
    </form>
  )
}
