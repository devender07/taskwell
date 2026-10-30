import { Outlet, createFileRoute, redirect } from '@tanstack/react-router'
import { getMe } from '@/api'
import { Sidebar } from '@/components/app/sidebar'

export const Route = createFileRoute('/_app')({
  beforeLoad: async ({ location }) => {
    const admin = await getMe()
    if (!admin) throw redirect({ to: '/signin', search: location.href === '/' ? {} : { redirect: location.href } })
    return { admin }
  },
  component: AppLayout,
})

function AppLayout() {
  const { admin } = Route.useRouteContext()
  return (
    <div className="shell">
      <Sidebar email={admin.email} />
      <Outlet />
    </div>
  )
}
