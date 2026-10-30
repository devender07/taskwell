import { Outlet, createFileRoute, redirect } from '@tanstack/react-router'
import { getMe } from '@/api'
import { Navbar } from '@/components/app/navbar'

// Pathless layout: everything under _app/ needs a session and gets the navbar.
export const Route = createFileRoute('/_app')({
  beforeLoad: async () => {
    const user = await getMe()
    if (!user) throw redirect({ to: '/signin' })
    return { user }
  },
  component: AppLayout,
})

function AppLayout() {
  const { user } = Route.useRouteContext()
  return (
    <>
      <Navbar user={user} />
      <Outlet />
    </>
  )
}
