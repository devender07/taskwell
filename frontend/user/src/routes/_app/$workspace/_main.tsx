import { Outlet, createFileRoute } from '@tanstack/react-router'
import { viewsPath, type SavedView } from '@/api'
import { loaderApi } from '@/realtime'
import { AppSidebar } from '@/components/app/sidebar'

// Pathless layout for the workspace's main pages: app sidebar + page. Settings has its own sidebar instead.
// Loads the saved views for the sidebar and the issue toolbars.
export const Route = createFileRoute('/_app/$workspace/_main')({
  loader: (ctx) => loaderApi<SavedView[]>(ctx, viewsPath(ctx.params.workspace)),
  component: MainLayout,
})

function MainLayout() {
  const { workspace } = Route.useParams()
  return (
    <div className="shell">
      <AppSidebar workspace={workspace} />
      <Outlet />
    </div>
  )
}
