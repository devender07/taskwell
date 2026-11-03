import { Link, createFileRoute } from '@tanstack/react-router'
import { ApiError, webhooksPath, type Webhook } from '@/api'
import { loaderApi } from '@/realtime'
import { AddWebhookButton, WebhookRow } from '@/components/settings/webhooks'

export const Route = createFileRoute('/_app/$workspace/_main/settings/webhooks/')({
  loader: (ctx) =>
    loaderApi<Webhook[]>(ctx, webhooksPath(ctx.params.workspace)).catch((e) => {
      if (e instanceof ApiError && e.status === 403) return null
      throw e
    }),
  component: WebhooksSettings,
})

function WebhooksSettings() {
  const { workspace } = Route.useParams()
  const webhooks = Route.useLoaderData()
  return (
    <main className="grid gap-6 w-full px-8 py-10" style={{ alignContent: 'start' }}>
      <header className="grid gap-2">
        <h1 className="text-h2">Webhooks</h1>
        <p className="text-body-sm text-secondary">
          Tether sends a signed POST to your URL when issues and comments change.{' '}
          <Link to="/docs/api" hash="webhooks" style={{ color: 'var(--link)' }}>
            Read the webhook docs
          </Link>
        </p>
      </header>
      {webhooks ? (
        <>
          <AddWebhookButton workspace={workspace} />
          {webhooks.length ? (
            <ul className="grid m-0 p-0 bg-surface border rounded-lg" style={{ listStyle: 'none' }} aria-label="Webhooks">
              {webhooks.map((w) => (
                <WebhookRow key={w.id} workspace={workspace} webhook={w} />
              ))}
            </ul>
          ) : (
            <p className="text-body-sm text-secondary m-0">No webhooks yet. Add one to get issue and comment events at your own endpoint.</p>
          )}
        </>
      ) : (
        <p className="text-body-sm text-secondary">You don't have permission to manage webhooks.</p>
      )}
    </main>
  )
}
