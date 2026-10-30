import { createFileRoute, redirect } from '@tanstack/react-router'
import { getMe } from '@/api'

// Pathless layout: everything under _app/ needs a session. No component, so it renders an <Outlet />.
export const Route = createFileRoute('/_app')({
  beforeLoad: async () => {
    const user = await getMe()
    if (!user) throw redirect({ to: '/signin' })
    return { user }
  },
})
