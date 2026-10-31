import { Outlet, createFileRoute, redirect, useParams } from '@tanstack/react-router'
import { api, getMe, type Workspace } from '@/api'
import { Navbar } from '@/components/app/navbar'
import { UnreadProvider } from '@/components/app/unread'
import { useRealtime } from '@/realtime'

// Pathless layout: everything under _app/ needs a session and gets the navbar.
// The workspace list lives in context (not a loader) so the `/` redirect can read it without a second request.
export const Route = createFileRoute('/_app')({
  beforeLoad: async () => {
    const workspaces = api<Workspace[]>('/workspaces') // in parallel with /me; both share one token refresh
    workspaces.catch(() => {}) // signed out: getMe decides, don't surface this rejection
    const user = await getMe()
    if (!user) throw redirect({ to: '/signin' })
    return { user, workspaces: await workspaces }
  },
  component: AppLayout,
})

function AppLayout() {
  const { user, workspaces } = Route.useRouteContext()
  useRealtime(useParams({ strict: false }).workspace)
  return (
    <UnreadProvider>
      <Navbar user={user} workspaces={workspaces} />
      <Outlet />
    </UnreadProvider>
  )
}
