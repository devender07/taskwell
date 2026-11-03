import { Link, createFileRoute } from '@tanstack/react-router'
import { ApiError, api } from '@/api'
import { AuthCard, FormError } from '@/components/auth/auth-card'
import { PublicFeedbackForm } from '@/components/feedback/public-form'

// Outside _app: customers send feedback here without an account.
export const Route = createFileRoute('/f/$token')({
  loader: async ({ params }) => {
    try {
      return await api<{ workspace: string }>(`/feedback-forms/${encodeURIComponent(params.token)}`)
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) return null
      throw e
    }
  },
  component: FeedbackFormPage,
})

function Unavailable() {
  return (
    <AuthCard title="Feedback form unavailable" subtitle="Ask whoever shared it for a new link." footer={<Link to="/" className="auth-link">Go to Tether</Link>}>
      <FormError>This feedback form isn't available.</FormError>
    </AuthCard>
  )
}

function FeedbackFormPage() {
  const { token } = Route.useParams()
  const info = Route.useLoaderData()
  return info ? <PublicFeedbackForm token={token} workspace={info.workspace} unavailable={<Unavailable />} /> : <Unavailable />
}
