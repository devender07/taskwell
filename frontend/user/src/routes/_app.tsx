import { Outlet, createFileRoute, redirect, useParams } from '@tanstack/react-router'
import { api, getMe, type User, type Workspace } from '@/api'
import { Navbar } from '@/components/app/navbar'
import { UnreadProvider } from '@/components/app/unread'
import { useRealtime } from '@/realtime'

// Pathless layout: everything under _app/ needs a session and gets the navbar.
// The workspace list lives in context (not a loader) so the `/` redirect can read it without a second request.
let last: { user: User; workspaces: Workspace[] } | null = null

export const Route = createFileRoute('/_app')({
  beforeLoad: async ({ preload }) => {
    if (preload && last) return last // beforeLoad runs on every hover preload; the session doesn't change on hover
    const workspaces = api<Workspace[]>('/workspaces') // in parallel with /me; both share one token refresh
    workspaces.catch(() => {}) // signed out: getMe decides, don't surface this rejection
    const user = await getMe()
    if (!user) throw redirect({ to: '/signin' })
    last = { user, workspaces: await workspaces }
    return last
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
