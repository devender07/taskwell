import { useNavigate } from '@tanstack/react-router'
import { useActionState } from 'react'
import { Button } from '@tether/shared/button'
import { Input } from '@tether/shared/input'
import { password } from '@tether/shared/validate'
import { api, toFormErrors, type FieldErrors } from '@/api'
import { FormError } from './auth-card'

type ResetState = { errors?: FieldErrors; error?: string | null }

export function ResetPasswordForm({ token }: { token: string }) {
  const navigate = useNavigate()
  const [state, submit, pending] = useActionState(async (_prev: ResetState, form: FormData): Promise<ResetState> => {
    const fieldError = password(String(form.get('password') ?? ''))
    if (fieldError) return { errors: { password: fieldError } }
    try {
      await api('/auth/password/reset', form)
    } catch (e) {
      return toFormErrors(e)
    }
    await navigate({ to: '/signin', search: { method: 'password', reset: true } })
    return {}
  }, {})

  return (
    <form action={submit} noValidate className="grid gap-4">
      <input type="hidden" name="token" value={token} />
      <Input
        label="New password"
        name="password"
        type="password"
        autoComplete="new-password"
        placeholder="At least 8 characters"
        autoFocus
        error={state.errors?.password}
      />
      <FormError>{state.error ?? state.errors?.token}</FormError>
      <Button type="submit" className="w-full" disabled={pending}>
        {pending ? 'Saving…' : 'Set new password'}
      </Button>
    </form>
  )
}
