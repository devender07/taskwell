import { Outlet, createFileRoute, notFound } from '@tanstack/react-router'
import { ApiError, type Workspace } from '@/api'
import { loaderApi } from '@/realtime'
import { Shortcuts } from '@/components/app/shortcuts'

// Children read the workspace with useLoaderData({ from: '/_app/$workspace' }); a 404 renders not found.
export const Route = createFileRoute('/_app/$workspace')({
  loader: async (ctx) => {
    try {
      return await loaderApi<Workspace>(ctx, `/workspaces/${encodeURIComponent(ctx.params.workspace)}`)
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) throw notFound()
      throw e
    }
  },
  component: WorkspaceLayout,
})

function WorkspaceLayout() {
  return (
    <Shortcuts>
      <Outlet />
    </Shortcuts>
  )
}
