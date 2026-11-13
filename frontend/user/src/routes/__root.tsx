import { Outlet, createRootRoute } from '@tanstack/react-router'
import { NavigationProgress } from '@taskwell/shared/boundaries'
import { getBeta } from '@/api'

export const Route = createRootRoute({
  beforeLoad: async () => ({ beta: await getBeta() }),
  component: () => (
    <>
      <NavigationProgress />
      <Outlet />
    </>
  ),
})
