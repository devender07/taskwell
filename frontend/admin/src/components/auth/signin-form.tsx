import { useNavigate } from '@tanstack/react-router'
import { useActionState } from 'react'
import { Button } from '@tether/shared/button'
import { Input } from '@tether/shared/input'
import { email as checkEmail, required } from '@tether/shared/validate'
import { api, toFormErrors, type FieldErrors } from '@/api'

type State = { email: string; errors?: FieldErrors; error?: string | null }

export function SigninForm({ redirect }: { redirect?: string }) {
  const navigate = useNavigate()
  const [state, submit, pending] = useActionState(async (_prev: State, form: FormData): Promise<State> => {
    const email = String(form.get('email') ?? '').trim()
    const errors = { email: checkEmail(email), password: required('Password')(String(form.get('password') ?? '')) }
    if (errors.email || errors.password) return { email, errors }
    form.set('email', email)
    try {
      await api('/auth/signin', form)
    } catch (e) {
      return { email, ...toFormErrors(e) }
    }
    await navigate({ href: redirect ?? '/' })
    return { email }
  }, { email: '' })

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
