import { createRouter } from '@tanstack/react-router'
import { ErrorFallback, NotFound, Pending } from '@tether/shared/boundaries'
import { routeTree } from './routeTree.gen'

export const router = createRouter({
  routeTree,
  defaultPreload: 'intent', // start loaders on hover/focus of any <Link>; a hovered route's data is reused for 30s (realtime events still invalidate it)
  defaultPendingComponent: Pending,
  defaultPendingMs: 200,
  defaultPendingMinMs: 300,
  defaultErrorComponent: ErrorFallback,
  defaultNotFoundComponent: NotFound,
  scrollRestoration: true,
})

// A same-URL load (router.invalidate) otherwise reloads every match, since staleTime is 0, so invalidate({ filter })
// reloads only the filtered ones: a match the page stays on reloads only when invalidated. Entering a page still loads it.
for (const route of Object.values(router.routesById)) {
  const options = route.options as { shouldReload?: (ctx: { cause: string }) => boolean | undefined }
  options.shouldReload ??= ({ cause }) => (cause === 'stay' ? false : undefined)
}

// Registers the router type so <Link>, useNavigate, useParams, useSearch are all typed.
declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}
