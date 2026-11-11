import { Outlet, createFileRoute, redirect, useParams } from '@tanstack/react-router'
import { ToastProvider } from '@taskwell/shared/toast'
import { api, billingPath, getMe, safeRedirect, useFeature, type Billing, type User, type Workspace } from '@/api'
import { useChatAlerts } from '@/components/app/chat-alert'
import { MenuProvider, Navbar } from '@/components/app/navbar'
import { ReauthDialog } from '@/components/app/reauth-dialog'
import { UnreadProvider } from '@/components/app/unread'
import { keepSession, useRealtime } from '@/realtime'

// Pathless layout: everything under _app/ needs a session and gets the navbar.
// The workspace list lives in context (not a loader) so pages and the navbar read it without a second request.
// billing: the user's own account, for the past-due banner; null when it can't load.
let last: { user: User; workspaces: Workspace[]; billing: Billing | null } | null = null

export const Route = createFileRoute('/_app')({
  beforeLoad: async ({ preload, location }) => {
    if ((preload || keepSession()) && last) return last // hover preloads and page-only reloads don't change the session
    const workspaces = api<Workspace[]>('/workspaces') // in parallel with /me; both share one token refresh
    workspaces.catch(() => {}) // signed out: getMe decides, don't surface this rejection
    const billing = api<Billing>(billingPath).catch(() => null)
    const user = await getMe()
    if (!user) throw redirect({ to: '/signin', search: { redirect: safeRedirect(location.href) } })
    last = { user, workspaces: await workspaces, billing: await billing }
    return last
  },
  component: AppLayout,
})

function AppLayout() {
  const { user, workspaces } = Route.useRouteContext()
  const workspace = useParams({ strict: false }).workspace
  useRealtime(workspace)
  useChatAlerts(useFeature('chat') ? workspace : undefined, user.id)
  return (
    <ToastProvider>
      <UnreadProvider>
        <MenuProvider>
          <Navbar user={user} workspaces={workspaces} />
          <Outlet />
          <ReauthDialog />
        </MenuProvider>
      </UnreadProvider>
    </ToastProvider>
  )
}
