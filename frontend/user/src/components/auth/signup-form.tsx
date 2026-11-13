import { Link, useNavigate } from '@tanstack/react-router'
import { useActionState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Checkbox } from '@taskwell/shared/checkbox'
import { Input } from '@taskwell/shared/input'
import { email, name, password, workspaceName } from '@taskwell/shared/validate'
import { api, homePath, toFormErrors, type FieldErrors } from '@/api'
import { FormError } from './auth-card'

type Values = { name: string; email: string; workspace: string; accepted: boolean }
type SignupState = {
  step: 'details' | 'code' | 'done'
  values: Values
  signup?: FormData // re-posted by "Resend code"
  resent?: boolean
  errors?: FieldErrors
  error?: string | null
}

async function signupAction(prev: SignupState, form: FormData, invited: boolean): Promise<SignupState> {
  const intent = form.get('intent')
  if (intent === 'change') return { step: 'details', values: prev.values }
  if (intent === 'resend' && prev.signup) {
    try {
      await api('/auth/signup', prev.signup)
    } catch (e) {
      return { ...prev, ...toFormErrors(e) }
    }
    return { ...prev, resent: true, errors: {}, error: null }
  }
  if (prev.step === 'details') {
    const get = (key: string) => String(form.get(key) ?? '').trim()
    const values = { name: get('name'), email: get('email'), workspace: get('workspace_name'), accepted: form.get('accept_terms') === 'true' }
    const pw = String(form.get('password') ?? '')
    const errors = {
      name: name(values.name),
      email: email(values.email),
      workspace_name: invited ? null : workspaceName(values.workspace),
      password: password(pw),
      password_confirm: pw === String(form.get('password_confirm') ?? '') ? null : "Passwords don't match.",
      accept_terms: values.accepted ? null : "Confirm you're 18 or older and accept the terms.",
    }
    if (Object.values(errors).some(Boolean)) return { step: 'details', values, errors }
    try {
      await api('/auth/signup', form)
    } catch (e) {
      return { step: 'details', values, ...toFormErrors(e) }
    }
    return { step: 'code', values, signup: form }
  }
  const code = String(form.get('code') ?? '').trim()
  if (!/^\d{6}$/.test(code)) return { ...prev, errors: { code: 'Enter the 6-digit code.' }, error: null }
  try {
    await api('/auth/signup/verify', form)
  } catch (e) {
    const { errors, error } = toFormErrors(e)
    return { ...prev, errors, error: error ?? errors.email }
  }
  return { ...prev, step: 'done' }
}

export function SignupForm({ redirect }: { redirect?: string }) {
  const navigate = useNavigate()
  const invited = redirect?.startsWith('/invite') ?? false // they join the inviter's workspace, so no name for their own
  const [state, submit, pending] = useActionState(async (prev: SignupState, form: FormData) => {
    const next = await signupAction(prev, form, invited)
    if (next.step === 'done') await navigate({ href: redirect ?? (await homePath()) })
    return next
  }, { step: 'details', values: { name: '', email: '', workspace: '', accepted: false } })
  const { values, errors, error } = state

  if (state.step !== 'details') {
    return (
      <form action={submit} noValidate className="grid gap-4">
        <div role="status" className="grid gap-1">
          <h2 className="text-body-sm text-primary" style={{ fontWeight: 590 }}>Check your email</h2>
          <p className="text-body-sm text-secondary">
            We sent a {state.resent ? 'new ' : ''}6-digit code to <strong className="text-primary font-510">{values.email}</strong>. Enter it to finish
            creating your account.
          </p>
        </div>
        <input type="hidden" name="email" value={values.email} />
        <Input
          label="Code"
          name="code"
          style={{ font: '400 20px/1 var(--mono)', letterSpacing: 8, textAlign: 'center' }}
          inputMode="numeric"
          autoComplete="one-time-code"
          maxLength={6}
          placeholder="000000"
          autoFocus
          error={errors?.code}
        />
        <FormError>{error}</FormError>
        <Button type="submit" className="w-full" disabled={pending}>
          {pending ? 'Verifying…' : 'Verify and continue'}
        </Button>
        <div className="flex justify-between">
          <Button variant="ghost" type="submit" name="intent" value="change" disabled={pending}>
            Change details
          </Button>
          <Button variant="ghost" type="submit" name="intent" value="resend" disabled={pending}>
            Resend code
          </Button>
        </div>
      </form>
    )
  }

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
      {invited ? null : (
        <Input
          label="Company or workspace name"
          name="workspace_name"
          autoComplete="organization"
          placeholder="Acme Inc"
          defaultValue={values.workspace}
          error={errors?.workspace_name}
        />
      )}
      <Input
        label="Password"
        name="password"
        type="password"
        autoComplete="new-password"
        placeholder="At least 8 characters"
        error={errors?.password}
      />
      <Input
        label="Confirm password"
        name="password_confirm"
        type="password"
        autoComplete="new-password"
        placeholder="Type it again"
        error={errors?.password_confirm}
      />
      <div className="grid gap-1">
        <Checkbox
          name="accept_terms"
          value="true"
          defaultChecked={values.accepted}
          aria-invalid={errors?.accept_terms ? true : undefined}
          aria-describedby={errors?.accept_terms ? 'accept-terms-error' : undefined}
          label={
            <span>
              I'm 18 or older and accept the{' '}
              <Link to="/terms" target="_blank" className="auth-link">
                terms
              </Link>{' '}
              and{' '}
              <Link to="/privacy" target="_blank" className="auth-link">
                privacy notice
              </Link>
            </span>
          }
        />
        {errors?.accept_terms ? (
          <p id="accept-terms-error" role="alert" className="text-caption text-accent">
            {errors.accept_terms}
          </p>
        ) : null}
      </div>
      <FormError>{error}</FormError>
      <Button type="submit" className="w-full" disabled={pending}>
        {pending ? 'Creating account…' : 'Create account'}
      </Button>
    </form>
  )
}
