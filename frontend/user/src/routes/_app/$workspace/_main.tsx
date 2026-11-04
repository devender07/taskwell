import { Navigate, Outlet, createFileRoute, useLoaderData, useLocation } from '@tanstack/react-router'
import { viewsPath, type SavedView } from '@/api'
import { loaderApi } from '@/realtime'
import { AppSidebar } from '@/components/app/sidebar'
import { guestBlocked } from '@/components/app/guest'

// Pathless layout for the workspace's main pages: app sidebar + page. Settings has its own sidebar instead.
// Loads the saved views for the sidebar and the issue toolbars.
export const Route = createFileRoute('/_app/$workspace/_main')({
  loader: (ctx) => loaderApi<SavedView[]>(ctx, viewsPath(ctx.params.workspace)),
  component: MainLayout,
})

function MainLayout() {
  const { workspace } = Route.useParams()
  const guest = useLoaderData({ from: '/_app/$workspace', select: (w) => w.guest })
  const blocked = useLocation({ select: (l) => guest === true && guestBlocked(l.pathname) })
  return (
    <div className="shell">
      <AppSidebar workspace={workspace} />
      {blocked ? <Navigate to="/$workspace" params={{ workspace }} replace /> : <Outlet />}
    </div>
  )
}
