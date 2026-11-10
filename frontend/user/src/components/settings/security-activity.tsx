import { useActionState, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { fullTime, relativeTime } from '@taskwell/shared/time'
import { accountEventsPath, api, toFormErrors, type AccountEventPage } from '@/api'
import { FormError } from '@/components/auth/auth-card'

const ACTIONS: Record<string, string> = {
  sign_in: 'Signed in',
  sign_in_failed: 'Failed sign-in',
  sign_out: 'Signed out',
  password_changed: 'Password changed',
  password_added: 'Password added',
  email_changed: 'Email changed',
  mfa_enabled: 'Two-factor turned on',
  mfa_disabled: 'Two-factor turned off',
  token_created: 'API token created',
  token_deleted: 'API token deleted',
}

export function SecurityActivity({ first }: { first: AccountEventPage | null }) {
  const [page, setPage] = useState(first)
  const events = page?.events ?? []
  const [error, more, pending] = useActionState(async () => {
    const last = events.at(-1)
    if (!last) return null
    try {
      const next = await api<AccountEventPage>(`${accountEventsPath}?before=${encodeURIComponent(last.id)}`)
      setPage((p) => ({ events: [...(p?.events ?? []), ...next.events], has_more: next.has_more }))
    } catch (e) {
      return toFormErrors(e).error
    }
    return null
  }, null)

  return (
    <div className="grid gap-4">
      <div className="grid gap-1">
        <h2 className="text-label">Security activity</h2>
        <p className="text-body-sm text-secondary">Sign-ins and changes to your account in the last year.</p>
      </div>
      {!page ? <FormError>Couldn't load security activity. Reload to try again.</FormError> : null}
      {page && !events.length ? <p className="text-body-sm text-secondary">No activity yet.</p> : null}
      {events.length ? (
        <ul className="grid gap-3 m-0 p-0" style={{ listStyle: 'none' }}>
          {events.map((e) => (
            <li key={e.id} className="grid gap-1">
              <div className="flex items-center justify-between gap-3">
                <span className="text-body-sm text-primary">
                  {ACTIONS[e.action] ?? e.action}
                  {e.detail ? <span className="text-secondary"> · {e.detail}</span> : null}
                </span>
                <time dateTime={e.at} title={fullTime(e.at)} className="text-caption text-secondary flex-noshrink">
                  {relativeTime(e.at)}
                </time>
              </div>
              <span className="text-caption text-secondary ellipsis" title={e.user_agent}>
                {[e.ip, e.user_agent].filter(Boolean).join(' · ')}
              </span>
            </li>
          ))}
        </ul>
      ) : null}
      <FormError>{error}</FormError>
      {page?.has_more ? (
        <form action={more}>
          <Button type="submit" variant="secondary" disabled={pending}>
            {pending ? 'Loading…' : 'Show more'}
          </Button>
        </form>
      ) : null}
    </div>
  )
}
