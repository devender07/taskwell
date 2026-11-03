import { Link, useNavigate } from '@tanstack/react-router'
import { useActionState } from 'react'
import { Button } from '@tether/shared/button'
import { Checkbox } from '@tether/shared/checkbox'
import { Input } from '@tether/shared/input'
import { email as checkEmail, required } from '@tether/shared/validate'
import { api, homePath, toFormErrors, type FieldErrors } from '@/api'
import { FormError } from './auth-card'

type PasswordState = { email: string; remember: boolean; errors?: FieldErrors; error?: string | null }

export function PasswordForm({ redirect }: { redirect?: string }) {
  const navigate = useNavigate()
  const [state, submit, pending] = useActionState(async (_prev: PasswordState, form: FormData): Promise<PasswordState> => {
    const email = String(form.get('email') ?? '').trim()
    const remember = form.has('remember')
    const errors = { email: checkEmail(email), password: required('Password')(String(form.get('password') ?? '')) }
    if (errors.email || errors.password) return { email, remember, errors }
    try {
      await api('/auth/signin', form)
    } catch (e) {
      return { email, remember, ...toFormErrors(e) }
    }
    await navigate({ href: redirect ?? (await homePath()) })
    return { email, remember }
  }, { email: '', remember: false })

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
      <div className="flex items-center justify-between">
        <Checkbox name="remember" label="Remember me" defaultChecked={state.remember} />
        <Link to="/forgot-password" className="auth-link text-caption">
          Forgot password?
        </Link>
      </div>
      <FormError>{state.error}</FormError>
      <Button type="submit" className="w-full" disabled={pending}>
        {pending ? 'Signing in…' : 'Sign in'}
      </Button>
    </form>
  )
}
