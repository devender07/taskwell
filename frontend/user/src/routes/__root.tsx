import { Outlet, createRootRoute } from '@tanstack/react-router'
import { NavigationProgress } from '@taskwell/shared/boundaries'
import { getAppInfo } from '@/api'

export const Route = createRootRoute({
  beforeLoad: () => getAppInfo(),
  component: () => (
    <>
      <NavigationProgress />
      <Outlet />
    </>
  ),
})
