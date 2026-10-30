import { useNavigate, useRouter } from '@tanstack/react-router'
import { useActionState } from 'react'
import { Button } from '@tether/shared/button'
import { Input } from '@tether/shared/input'
import { email, name, password } from '@tether/shared/validate'
import { api, toFormErrors, type FieldErrors } from '@/api'
import { FormError } from './auth-card'

type Values = { name: string; email: string }
type SignupState = { values: Values; errors?: FieldErrors; error?: string | null }

export function SignupForm() {
  const router = useRouter()
  const navigate = useNavigate()
  const [{ values, errors, error }, submit, pending] = useActionState(async (_prev: SignupState, form: FormData): Promise<SignupState> => {
    const get = (key: string) => String(form.get(key) ?? '').trim()
    const values = { name: get('name'), email: get('email') }
    const errors = { name: name(values.name), email: email(values.email), password: password(String(form.get('password') ?? '')) }
    if (Object.values(errors).some(Boolean)) return { values, errors }
    try {
      await api('/auth/signup', form)
    } catch (e) {
      return { values, ...toFormErrors(e) }
    }
    await router.invalidate()
    await navigate({ to: '/' })
    return { values }
  }, { values: { name: '', email: '' } })

  return (
    <form action={submit} noValidate className="grid gap-4">
      <Input label="Full name" name="name" autoComplete="name" placeholder="Ada Lovelace" defaultValue={values.name} error={errors?.name} />
      <Input
        label="Work email"
        name="email"
        type="email"
        autoComplete="email"
        placeholder="you@company.com"
        defaultValue={values.email}
        error={errors?.email}
      />
      <Input
        label="Password"
        name="password"
        type="password"
        autoComplete="new-password"
        placeholder="At least 8 characters"
        error={errors?.password}
      />
      <FormError>{error}</FormError>
      <Button type="submit" className="w-full" disabled={pending}>
        {pending ? 'Creating account…' : 'Create account'}
      </Button>
    </form>
  )
}
