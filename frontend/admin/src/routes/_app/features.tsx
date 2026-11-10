import { createFileRoute } from '@tanstack/react-router'
import { PLANS, type Setting } from '@/api'
import { loaderApi } from '@/realtime'
import { FeatureRow } from '@/components/config/feature-row'

export const Route = createFileRoute('/_app/features')({
  loader: async (ctx) => (await loaderApi<Setting[]>(ctx, '/config')).filter((s) => s.key.startsWith('feature_')),
  component: Features,
})

function Features() {
  const settings = Route.useLoaderData()
  const features = settings.flatMap((feature) => {
    const plans = settings.find((s) => s.key === `${feature.key}_plans`)
    return plans ? [{ feature, plans }] : []
  })
  return (
    <main className="grid gap-8 w-full px-6 py-10" style={{ maxWidth: 1040 }}>
      <header className="grid gap-2">
        <h1 className="text-h2">Features</h1>
        <p className="text-body-sm text-secondary">
          Turn a feature off for everyone, or choose which plans get it while it's on. Data is kept while a feature is off.
        </p>
      </header>
      {features.length ? (
        <div className="table-wrap bg-surface border rounded-lg">
          <table className="table">
            <thead>
              <tr>
                <th>Feature</th>
                <th>On</th>
                {Object.values(PLANS).map((name) => (
                  <th key={name}>{name}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {features.map(({ feature, plans }) => (
                <FeatureRow key={feature.key} feature={feature} plans={plans} />
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <p className="text-body-sm text-secondary">No features to show.</p>
      )}
    </main>
  )
}
