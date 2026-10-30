import { Link, createFileRoute, redirect } from '@tanstack/react-router'
import { ApiError, api, getMe, toFormErrors, type Workspace } from '@/api'
import { AuthCard, FormError } from '@/components/auth/auth-card'
import { JoinWorkspace, SwitchAccount } from '@/components/invite/invite-actions'

type InviteInfo = { workspace: string; email: string; invited_by: string }

// Outside _app and (auth): the emailed link has to work signed in or out. Signed in with the invited email, it joins straight away.
export const Route = createFileRoute('/invite')({
  validateSearch: (search): { token?: string } => (typeof search.token === 'string' && search.token ? { token: search.token } : {}),
  loaderDeps: ({ search }) => ({ token: search.token }),
  loader: async ({ deps: { token } }) => {
    const [invite, me] = await Promise.all([token ? getInvite(token) : null, getMe()])
    if (!token || !invite || !me || me.email.toLowerCase() !== invite.email.toLowerCase()) return { invite, me, error: null }
    let workspace: Workspace
    try {
      workspace = await api<Workspace>(`/invites/${encodeURIComponent(token)}/accept`, new FormData())
    } catch (e) {
      return { invite, me, error: toFormErrors(e).error }
    }
    throw redirect({ to: '/$workspace', params: { workspace: workspace.slug }, replace: true })
  },
  gcTime: 0, // back from sign-in, show the signed-in state at once rather than a cached signed-out one
  component: InvitePage,
})

async function getInvite(token: string) {
  try {
    return await api<InviteInfo>(`/invites/${encodeURIComponent(token)}`)
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) return null
    throw e
  }
}

function InvitePage() {
  const { token } = Route.useSearch()
  const { invite, me, error } = Route.useLoaderData()

  if (!token || !invite) {
    return (
      <AuthCard
        title="Invite unavailable"
        subtitle="Ask whoever invited you to send a new link."
        footer={<Link to="/" className="auth-link">Go to Tether</Link>}
      >
        <FormError>This invite link is invalid or has expired.</FormError>
      </AuthCard>
    )
  }

  const here = `/invite?token=${encodeURIComponent(token)}`
  const strong = (text: string) => <strong className="text-primary font-510">{text}</strong>

  return (
    <AuthCard
      title={`Join ${invite.workspace}`}
      subtitle={invite.invited_by ? `${invite.invited_by} invited you to collaborate on Tether.` : "You've been invited to collaborate on Tether."}
    >
      {!me ? (
        <>
          <p className="text-body-sm text-secondary text-center">
            Sign in or create an account with {strong(invite.email)} to accept.
          </p>
          <div className="grid gap-3">
            <Link to="/signin" search={{ redirect: here }} className="button button-primary w-full">
              Sign in
            </Link>
            <Link to="/signup" search={{ redirect: here }} className="button button-secondary w-full">
              Create account
            </Link>
          </div>
        </>
      ) : me.email.toLowerCase() === invite.email.toLowerCase() ? (
        <>
          <FormError>{error}</FormError>
          <JoinWorkspace token={token} />
        </>
      ) : (
        <>
          <p className="text-body-sm text-secondary text-center">
            You're signed in as {strong(me.email)}. This invite is for {strong(invite.email)}.
          </p>
          <SwitchAccount redirect={here} />
        </>
      )}
    </AuthCard>
  )
}
