import { Outlet, createFileRoute, notFound, useRouteContext } from '@tanstack/react-router'
import { ApiError, api, getFeaturePlans, getFeatures, upgradesFor, type Features, type PersonRef, type Upgrades, type Workspace, type WorkspaceBilling, workspaceBillingPath, workspacePath } from '@/api'
import { keepSession, loaderApi } from '@/realtime'
import { PastDueBanner } from '@/components/app/past-due-banner'
import { TrialBanner } from '@/components/app/trial-banner'
import { Shortcuts } from '@/components/app/shortcuts'
import { InvitePrompt } from '@/components/invite/invite-prompt'
import { ReviewTokensPrompt } from '@/components/settings/review-tokens'

// Children read the workspace with useLoaderData({ from: '/_app/$workspace' }); a 404 renders not found.
// Its plan's feature flags live in context so child guards (requireFeature) can read them; refetched like _app's session.
// upgrades: features locked behind a higher plan (empty for guests, whose billing is 403); owner/is_owner say who can upgrade.
type Plan = { slug: string; features: Features; upgrades: Upgrades; owner: PersonRef | null; is_owner: boolean }
let last: Plan | null = null

export const Route = createFileRoute('/_app/$workspace')({
  beforeLoad: async ({ params, preload }) => {
    if ((preload || keepSession()) && last?.slug === params.workspace) return last
    const [features, plans, billing] = await Promise.all([
      getFeatures(params.workspace),
      getFeaturePlans(),
      api<WorkspaceBilling>(workspaceBillingPath(params.workspace)).catch(() => null),
    ])
    last = { slug: params.workspace, features, upgrades: upgradesFor(features, plans, billing?.plan), owner: billing?.owner ?? null, is_owner: billing?.is_owner ?? false }
    return last
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
  const billing = useRouteContext({ from: '/_app', select: (c) => c.billing })
  return (
    <Shortcuts>
      {billing?.status === 'past_due' ? <PastDueBanner plan={billing.plan} /> : null}
      {billing?.needs_payment_method ? <TrialBanner billing={billing} /> : null}
      <Outlet />
      <InvitePrompt slug={workspace} />
      <ReviewTokensPrompt slug={workspace} />
    </Shortcuts>
  )
}
