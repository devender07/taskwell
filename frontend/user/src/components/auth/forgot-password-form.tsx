import { useActionState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Input } from '@taskwell/shared/input'
import { email as checkEmail } from '@taskwell/shared/validate'
import { api, toFormErrors, type FieldErrors } from '@/api'
import { FormError } from './auth-card'

type ForgotState = { email: string; sent: boolean; errors?: FieldErrors; error?: string | null }

async function forgotAction(prev: ForgotState, form: FormData): Promise<ForgotState> {
  if (form.get('intent') === 'change') return { email: prev.email, sent: false }
  const email = String(form.get('email') ?? '').trim()
  const fieldError = checkEmail(email)
  if (fieldError) return { email, sent: false, errors: { email: fieldError } }
  try {
    await api('/auth/password/forgot', form)
  } catch (e) {
    return { email, sent: false, ...toFormErrors(e) }
  }
  return { email, sent: true }
}

export function ForgotPasswordForm() {
  const [state, submit, pending] = useActionState(forgotAction, { email: '', sent: false })

  if (state.sent) {
    return (
      <form action={submit} className="grid gap-4">
        <p role="status" className="text-body-sm text-secondary text-center">
          If an account exists for <strong className="text-primary font-510">{state.email}</strong>, you'll get a reset link shortly.
        </p>
        <Button variant="ghost" type="submit" name="intent" value="change" className="w-full">
          Use a different email
        </Button>
      </form>
    )
  }

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
        {pending ? 'Sending…' : 'Send reset link'}
      </Button>
    </form>
  )
}
