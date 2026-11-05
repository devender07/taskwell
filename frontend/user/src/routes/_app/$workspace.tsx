import { Outlet, createFileRoute, notFound } from '@tanstack/react-router'
import { ApiError, type Workspace, workspacePath } from '@/api'
import { loaderApi } from '@/realtime'
import { Shortcuts } from '@/components/app/shortcuts'
import { InvitePrompt } from '@/components/invite/invite-prompt'

// Children read the workspace with useLoaderData({ from: '/_app/$workspace' }); a 404 renders not found.
export const Route = createFileRoute('/_app/$workspace')({
  loader: async (ctx) => {
    try {
      return await loaderApi<Workspace>(ctx, `${workspacePath(ctx.params.workspace)}`)
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) throw notFound()
      throw e
    }
  },
  component: WorkspaceLayout,
})

function WorkspaceLayout() {
  const { workspace } = Route.useParams()
  return (
    <Shortcuts>
      <Outlet />
      <InvitePrompt slug={workspace} />
    </Shortcuts>
  )
}
