import { createFileRoute, notFound } from '@tanstack/react-router'
import { BackLink } from '@taskwell/shared/back-link'
import { ApiError, deliveriesPath, webhooksPath, type Webhook, type WebhookDelivery, requireFeature } from '@/api'
import { loaderApi } from '@/realtime'
import { ActiveToggle, EventChips } from '@/components/settings/webhooks'
import { Deliveries, WebhookActions } from '@/components/settings/webhooks-detail'
import { byId } from '@taskwell/shared/list'

export const Route = createFileRoute('/_app/$workspace/_main/settings/webhooks/$id')({
  beforeLoad: requireFeature('webhooks'),
  loader: async (ctx) => {
    const { workspace, id } = ctx.params
    const [webhooks, deliveries] = await Promise.all([
      loaderApi<Webhook[]>(ctx, webhooksPath(workspace)),
      loaderApi<WebhookDelivery[]>(ctx, deliveriesPath(workspace, id)).catch((e) => {
        if (e instanceof ApiError && e.status === 404) return null
        throw e
      }),
    ])
    const webhook = byId(webhooks, id)
    if (!webhook || !deliveries) throw notFound()
    return { webhook, deliveries }
  },
  component: WebhookDetail,
})

function WebhookDetail() {
  const { workspace } = Route.useParams()
  const { webhook, deliveries } = Route.useLoaderData()
  return (
    <main className="grid gap-6 w-full px-8 py-10" style={{ alignContent: 'start' }}>
      <header className="page-top grid gap-3">
        <BackLink to="/$workspace/settings/webhooks" params={{ workspace }}>
          Webhooks
        </BackLink>
        <h1 className="text-h2 m-0" style={{ wordBreak: 'break-all' }}>
          {webhook.url}
        </h1>
        <EventChips events={webhook.events} />
        <ActiveToggle workspace={workspace} webhook={webhook} />
      </header>
      <WebhookActions workspace={workspace} webhook={webhook} />
      <section className="grid gap-3" aria-labelledby="deliveries-heading">
        <h2 id="deliveries-heading" className="text-label m-0">
          Recent deliveries
        </h2>
        <Deliveries workspace={workspace} webhook={webhook.id} deliveries={deliveries} />
      </section>
    </main>
  )
}
