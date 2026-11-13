import { Link, createFileRoute, notFound, redirect } from '@tanstack/react-router'
import { LockIcon } from '@taskwell/shared/icons'
import type { Feature } from '@/api'
import { FEATURES } from '@/pricing/pricing-table'
import { planName } from '@/pricing/tiers'

export const Route = createFileRoute('/_app/$workspace/_main/upgrade')({
  validateSearch: (search): { feature?: Feature } => ({
    feature: FEATURES.find(([f]) => f === search.feature)?.[0],
  }),
  beforeLoad: ({ context, params, search }) => {
    if (context.beta) throw notFound()
    const plan = search.feature && context.upgrades[search.feature]
    if (!plan) throw redirect({ to: '/$workspace', params, replace: true })
    return { plan }
  },
  component: UpgradePage,
})

function UpgradePage() {
  const { feature } = Route.useSearch()
  const { plan, is_owner, owner } = Route.useRouteContext()
  const [, label, description] = FEATURES.find(([f]) => f === feature)!
  const name = planName(plan)

  return (
    <main className="grid w-full px-6 py-10" style={{ alignContent: 'start', justifyItems: 'center' }}>
      <section className="grid gap-4 p-8 bg-surface border rounded-lg w-full" style={{ maxWidth: 480, justifyItems: 'start' }} aria-labelledby="upgrade-title">
        <LockIcon size={24} className="text-secondary" />
        <div className="grid gap-2">
          <h1 id="upgrade-title" className="text-h2 m-0">{label}</h1>
          <p className="text-body-sm text-secondary m-0">{description}</p>
        </div>
        <p className="text-label m-0">{plan === 'advanced' ? `Available on ${name}` : `Available on ${name} and up`}</p>
        {is_owner ? (
          <Link to="/pricing" className="button button-primary">
            See plans
          </Link>
        ) : (
          <p className="text-body-sm text-secondary m-0">{owner ? `Ask ${owner.name} to upgrade the workspace.` : 'Ask the workspace owner to upgrade the workspace.'}</p>
        )}
      </section>
    </main>
  )
}
