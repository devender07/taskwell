import { createFileRoute, redirect } from '@tanstack/react-router'
import { ApiError, api, safeRedirect, toFormErrors } from '@/api'
import { AuthCard, FormError } from '@/components/auth/auth-card'
import { OAuthConsent, OAUTH_FIELDS, type OAuthQuery } from '@/components/auth/oauth-consent'

// Opened by an MCP client in the user's browser. Signed out, it goes to sign-in and comes back to this exact URL.
// ponytail: the router JSON-parses the query, so an all-digit value over 15 digits (e.g. a numeric state) loses
// precision; read window.location.search instead if a client ever sends one.
export const Route = createFileRoute('/(auth)/oauth/authorize')({
  validateSearch: (search): OAuthQuery =>
    Object.fromEntries(OAUTH_FIELDS.flatMap((k) => (search[k] == null ? [] : [[k, String(search[k])]]))),
  loaderDeps: ({ search }) => search,
  loader: async ({ deps, location }) => {
    try {
      return { client: await api<{ client_name: string; redirect_uri: string }>(`/oauth/authorize?${new URLSearchParams(deps)}`) }
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) throw redirect({ to: '/signin', search: { redirect: safeRedirect(location.href) } })
      return { error: toFormErrors(e).error }
    }
  },
  gcTime: 0,
  component: Authorize,
})

function Authorize() {
  const { client, error } = Route.useLoaderData()
  const query = Route.useSearch()
  if (!client) {
    return (
      <AuthCard title="Can't connect to Taskwell" subtitle="Go back to the app that sent you here and try again.">
        <FormError>{error}</FormError>
      </AuthCard>
    )
  }
  return (
    <AuthCard
      title={`Connect ${client.client_name} to Taskwell?`}
      subtitle={`It will act as you: see and change the issues, comments and chats you can. It sends you back to ${new URL(client.redirect_uri).host}.`}
    >
      <OAuthConsent query={query} />
    </AuthCard>
  )
}
