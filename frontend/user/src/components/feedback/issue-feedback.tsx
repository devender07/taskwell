import { fullTime, relativeTime } from '@taskwell/shared/time'
import { issuePath, type FeedbackPage, type Workspace } from '@/api'
import { FormError } from '@/components/auth/auth-card'
import { useLive } from '@/components/tests/tests'
import { feedbackWho } from './feedback'
import { useFeedbackPages } from './feedback-list'

export function IssueFeedback({ workspace, identifier }: { workspace: Workspace; identifier: string }) {
  const path = `${issuePath(workspace, identifier)}/feedback`
  const first = useLive<FeedbackPage>(path).data
  return first?.items.length ? <FeedbackSection path={path} first={first} /> : null
}

// Page one is live; scrolling to the end loads older feedback.
function FeedbackSection({ path, first }: { path: string; first: FeedbackPage }) {
  const { items, hasMore, end, error } = useFeedbackPages(path, first)
  return (
    <section className="grid gap-4" aria-labelledby="customer-feedback-heading">
      <h2 id="customer-feedback-heading" className="text-label">
        Customer feedback <span className="text-secondary">{first.total ?? items.length}</span>
      </h2>
      <ul className="grid m-0 p-0 bg-surface border rounded-lg overflow-hidden" style={{ listStyle: 'none' }}>
        {items.map((f, i) => (
          <li key={f.id} className="grid gap-1 px-4 py-3" style={i ? { borderTop: '1px solid rgba(255, 255, 255, 0.08)' } : undefined}>
            <div className="flex flex-wrap items-center gap-2 text-caption text-secondary">
              <span className="text-label text-primary">{feedbackWho(f)}</span>
              <span aria-hidden>·</span>
              <time dateTime={f.created_at} title={fullTime(f.created_at)}>
                {relativeTime(f.created_at)}
              </time>
              {f.archived ? <span>· Archived</span> : null}
            </div>
            <p className="text-body-sm m-0 ellipsis-3" style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>
              {f.body}
            </p>
          </li>
        ))}
      </ul>
      {hasMore ? <div ref={end} aria-hidden /> : null}
      <FormError>{error}</FormError>
    </section>
  )
}
