import { useActionState } from 'react'
import { Button } from '@tether/shared/button'
import { api, toFormErrors } from '@/api'
import { FormError } from './auth-card'

export const OAUTH_FIELDS = ['response_type', 'client_id', 'redirect_uri', 'code_challenge', 'code_challenge_method', 'state', 'scope'] as const
export type OAuthQuery = Partial<Record<(typeof OAUTH_FIELDS)[number], string>>

type State = { error?: string | null; done?: boolean }

export function OAuthConsent({ query }: { query: OAuthQuery }) {
  const [state, decide, pending] = useActionState(async (_prev: State, submitted: FormData): Promise<State> => {
    const form = new FormData()
    for (const [k, v] of Object.entries(query)) form.set(k, v)
    form.set('allow', submitted.get('allow') === 'true' ? 'true' : 'false')
    try {
      const { redirect } = await api<{ redirect: string }>('/oauth/authorize', form)
      window.location.assign(redirect)
      return { done: true }
    } catch (e) {
      return { error: toFormErrors(e).error }
    }
  }, {})

  if (state.done) {
    return (
      <p role="status" className="text-body-sm text-success text-center">
        You can close this tab.
      </p>
    )
  }
  return (
    <form action={decide} noValidate className="grid gap-3">
      <FormError>{state.error}</FormError>
      <Button type="submit" name="allow" value="true" className="w-full" disabled={pending} autoFocus>
        Allow
      </Button>
      <Button type="submit" name="allow" value="false" variant="ghost" className="w-full" disabled={pending}>
        Cancel
      </Button>
    </form>
  )
}
