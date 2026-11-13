import { createFileRoute } from '@tanstack/react-router'
import { ApiError, feedbackFormPath, type FeedbackForm, requireFeature } from '@/api'
import { loaderApi } from '@/realtime'
import { FeedbackFormCard } from '@/components/settings/feedback-form'

export const Route = createFileRoute('/_app/$workspace/_main/settings/feedback')({
  beforeLoad: requireFeature('feedback'),
  loader: (ctx) =>
    loaderApi<FeedbackForm>(ctx, feedbackFormPath(ctx.params.workspace)).catch((e) => {
      if (e instanceof ApiError && e.status === 403) return null
      throw e
    }),
  component: FeedbackSettings,
})

function FeedbackSettings() {
  const { workspace } = Route.useParams()
  const form = Route.useLoaderData()
  return (
    <main className="grid gap-6 w-full px-8 py-10" style={{ alignContent: 'start' }}>
      <header className="page-top grid gap-2">
        <h1 className="text-h2">Feedback form</h1>
        <p className="text-body-sm text-secondary">A public page where customers send feedback straight to your Feedback inbox.</p>
      </header>
      {form ? <FeedbackFormCard workspace={workspace} form={form} /> : <p className="text-body-sm text-secondary">You don't have permission to manage the feedback form.</p>}
    </main>
  )
}
