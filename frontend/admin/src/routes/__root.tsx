import { Outlet, createRootRoute } from '@tanstack/react-router'
import { NavigationProgress } from '@taskwell/shared/boundaries'

export const Route = createRootRoute({
  component: () => (
    <>
      <NavigationProgress />
      <Outlet />
    </>
  ),
})
