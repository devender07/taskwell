import { useActionState, type ReactNode } from 'react'
import { Button } from '@taskwell/shared/button'
import { Input } from '@taskwell/shared/input'
import { Textarea } from '@taskwell/shared/textarea'
import { feedbackBody, feedbackEmail, feedbackName } from '@taskwell/shared/validate'
import { ApiError, api, toFormErrors, type FieldErrors } from '@/api'
import { AuthCard, FormError } from '@/components/auth/auth-card'

type State = { values: Record<'email' | 'name' | 'body', string>; sent?: boolean; gone?: boolean; errors?: FieldErrors; error?: string | null }
const blank: State = { values: { email: '', name: '', body: '' } }

export function PublicFeedbackForm({ token, workspace, unavailable }: { token: string; workspace: string; unavailable: ReactNode }) {
  const [state, submit, pending] = useActionState(async (prev: State, form: FormData): Promise<State> => {
    if (form.get('intent') === 'more') return blank
    const values = { email: String(form.get('email') ?? ''), name: String(form.get('name') ?? ''), body: String(form.get('body') ?? '') }
    const errors = { email: feedbackEmail(values.email, true), name: feedbackName(values.name), body: feedbackBody(values.body) }
    if (Object.values(errors).some(Boolean)) return { values, errors }
    for (const k of ['email', 'name', 'body'] as const) form.set(k, values[k].trim())
    try {
      await api(`/feedback-forms/${encodeURIComponent(token)}`, form)
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) return { ...prev, gone: true }
      return { values, ...toFormErrors(e) }
    }
    return { ...blank, sent: true }
  }, blank)

  if (state.gone) return unavailable
  if (state.sent) {
    return (
      <AuthCard title={`Send feedback to ${workspace}`} subtitle="Tell the team what you need or what isn't working.">
        <form action={submit} className="grid gap-4">
          <p role="status" className="text-body-sm text-success text-center">
            Thanks, {workspace} got your feedback.
          </p>
          <Button type="submit" variant="secondary" name="intent" value="more" className="w-full">
            Send more
          </Button>
        </form>
      </AuthCard>
    )
  }

  const { values, errors } = state
  return (
    <AuthCard title={`Send feedback to ${workspace}`} subtitle="Tell the team what you need or what isn't working.">
      <form action={submit} noValidate className="grid gap-4">
        <Input label="Email" name="email" type="email" autoComplete="email" placeholder="you@company.com" defaultValue={values.email} error={errors?.email} />
        <Input label="Name (optional)" name="name" autoComplete="name" defaultValue={values.name} error={errors?.name} />
        <Textarea label="Message" name="body" rows={6} defaultValue={values.body} error={errors?.body} />
        <div aria-hidden className="sr-only">
          <label>
            Website
            <input name="website" tabIndex={-1} autoComplete="off" />
          </label>
        </div>
        <FormError>{state.error}</FormError>
        <Button type="submit" className="w-full" disabled={pending}>
          {pending ? 'Sending…' : 'Send feedback'}
        </Button>
      </form>
    </AuthCard>
  )
}
