import { Outlet, createFileRoute, redirect, useParams } from '@tanstack/react-router'
import { api, getMe, safeRedirect, type User, type Workspace } from '@/api'
import { useChatAlerts } from '@/components/app/chat-alert'
import { Navbar } from '@/components/app/navbar'
import { UnreadProvider } from '@/components/app/unread'
import { keepSession, useRealtime } from '@/realtime'

// Pathless layout: everything under _app/ needs a session and gets the navbar.
// The workspace list lives in context (not a loader) so the `/` redirect can read it without a second request.
let last: { user: User; workspaces: Workspace[] } | null = null

export const Route = createFileRoute('/_app')({
  beforeLoad: async ({ preload, location }) => {
    if ((preload || keepSession()) && last) return last // hover preloads and page-only reloads don't change the session
    const workspaces = api<Workspace[]>('/workspaces') // in parallel with /me; both share one token refresh
    workspaces.catch(() => {}) // signed out: getMe decides, don't surface this rejection
    const user = await getMe()
    if (!user) throw redirect({ to: '/signin', search: { redirect: location.href === '/' ? undefined : safeRedirect(location.href) } })
    last = { user, workspaces: await workspaces }
    return last
  },
  component: AppLayout,
})

function AppLayout() {
  const { user, workspaces } = Route.useRouteContext()
  const workspace = useParams({ strict: false }).workspace
  useRealtime(workspace)
  useChatAlerts(workspace, user.id)
  return (
    <UnreadProvider>
      <Navbar user={user} workspaces={workspaces} />
      <Outlet />
    </UnreadProvider>
  )
}
