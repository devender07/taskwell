import { Outlet, createFileRoute, notFound } from '@tanstack/react-router'
import { ApiError, getFeatures, type Features, type Workspace, workspacePath } from '@/api'
import { keepSession, loaderApi } from '@/realtime'
import { Shortcuts } from '@/components/app/shortcuts'
import { InvitePrompt } from '@/components/invite/invite-prompt'
import { ReviewTokensPrompt } from '@/components/settings/review-tokens'

// Children read the workspace with useLoaderData({ from: '/_app/$workspace' }); a 404 renders not found.
// Its plan's feature flags live in context so child guards (requireFeature) can read them; refetched like _app's session.
let last: { slug: string; features: Features } | null = null

export const Route = createFileRoute('/_app/$workspace')({
  beforeLoad: async ({ params, preload }) => {
    if ((preload || keepSession()) && last?.slug === params.workspace) return { features: last.features }
    last = { slug: params.workspace, features: await getFeatures(params.workspace) }
    return { features: last.features }
  },
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
      <ReviewTokensPrompt slug={workspace} />
    </Shortcuts>
  )
}
