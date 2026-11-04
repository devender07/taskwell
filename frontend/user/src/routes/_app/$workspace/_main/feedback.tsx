import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { Button } from '@taskwell/shared/button'
import { Input } from '@taskwell/shared/input'
import { Tabs } from '@taskwell/shared/tabs'
import { feedbackListPath, type FeedbackPage, type FeedbackState } from '@/api'
import { loaderApi } from '@/realtime'
import { AddFeedbackDialog, FeedbackList } from '@/components/feedback/feedback-list'

const states: { value: FeedbackState; label: string }[] = [
  { value: 'new', label: 'New' },
  { value: 'linked', label: 'Linked' },
  { value: 'archived', label: 'Archived' },
]

type Search = { state?: 'linked' | 'archived'; q?: string; add?: true }

export const Route = createFileRoute('/_app/$workspace/_main/feedback')({
  // new, the default, stays out of the URL; `add` opens the Add feedback dialog (the command palette links to it).
  validateSearch: (search): Search => ({
    ...((search.state === 'linked' || search.state === 'archived') && { state: search.state }),
    ...(typeof search.q === 'string' && search.q && { q: search.q }),
    ...(search.add === true && { add: true }),
  }),
  loaderDeps: ({ search }) => ({ state: search.state ?? ('new' as const), q: search.q?.trim() ?? '' }),
  loader: (ctx) => loaderApi<FeedbackPage>(ctx, feedbackListPath(ctx.params.workspace, ctx.deps.state, ctx.deps.q)),
  component: FeedbackPageView,
})

function FeedbackPageView() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const page = Route.useLoaderData()
  const { state = 'new', q = '', add } = Route.useSearch()
  const navigate = Route.useNavigate()

  return (
    <main className="grid gap-6 w-full px-6 py-10" style={{ alignContent: 'start', minWidth: 0 }}>
      <header className="flex flex-wrap items-center justify-between gap-4">
        <h1 className="text-h2">Feedback</h1>
        <Button onClick={() => navigate({ search: (prev) => ({ ...prev, add: true }), resetScroll: false })}>Add feedback</Button>
      </header>
      <div className="flex flex-wrap items-center gap-3">
        <Tabs
          aria-label="Feedback state"
          items={states}
          value={state}
          onChange={(v) => navigate({ search: (prev) => ({ ...prev, state: v === 'new' ? undefined : v }), replace: true })}
        />
        <div className="flex-grow" style={{ minWidth: 200 }}>
          <Input
            type="search"
            aria-label="Search feedback"
            placeholder="Search by message, email or name"
            defaultValue={q}
            onDebouncedChange={(next) => navigate({ search: (prev) => ({ ...prev, q: next.trim() || undefined }), replace: true })}
          />
        </div>
      </div>
      <FeedbackList key={`${state} ${q.trim()}`} workspace={workspace} state={state} q={q.trim()} page={page} />
      <AddFeedbackDialog
        workspace={workspace.slug}
        open={!!add}
        onClose={() => navigate({ search: ({ add: _, ...prev }) => prev, replace: true, resetScroll: false })}
      />
    </main>
  )
}
