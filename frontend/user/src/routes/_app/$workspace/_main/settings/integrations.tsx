import { createFileRoute } from '@tanstack/react-router'
import { ApiError, integrationsPath, type Integration, type Status } from '@/api'
import { loaderApi } from '@/realtime'
import { IntegrationCard } from '@/components/settings/integrations'

export const Route = createFileRoute('/_app/$workspace/_main/settings/integrations')({
  loader: async (ctx) => {
    const { workspace } = ctx.params
    const [integrations, statuses] = await Promise.all([
      loaderApi<Integration[]>(ctx, integrationsPath(workspace)).catch((e) => {
        if (e instanceof ApiError && e.status === 403) return null
        throw e
      }),
      loaderApi<Status[]>(ctx, `/workspaces/${encodeURIComponent(workspace)}/statuses`),
    ])
    return { integrations, statuses }
  },
  component: IntegrationsSettings,
})

function IntegrationsSettings() {
  const { workspace } = Route.useParams()
  const { integrations, statuses } = Route.useLoaderData()
  return (
    <main className="grid gap-6 w-full px-8 py-10" style={{ alignContent: 'start' }}>
      <header className="grid gap-2">
        <h1 className="text-h2">Integrations</h1>
        <p className="text-body-sm text-secondary">Link pull requests and merge requests to the issues they mention.</p>
      </header>
      {integrations ? (
        integrations.map((i) => <IntegrationCard key={i.provider} workspace={workspace} integration={i} statuses={statuses} />)
      ) : (
        <p className="text-body-sm text-secondary">You don't have permission to manage integrations.</p>
      )}
    </main>
  )
}
