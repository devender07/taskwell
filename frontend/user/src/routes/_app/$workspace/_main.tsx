import { Outlet, createFileRoute } from '@tanstack/react-router'
import { AppSidebar } from '@/components/app/sidebar'

// Pathless layout for the workspace's main pages: app sidebar + page. Settings has its own sidebar instead.
export const Route = createFileRoute('/_app/$workspace/_main')({
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
