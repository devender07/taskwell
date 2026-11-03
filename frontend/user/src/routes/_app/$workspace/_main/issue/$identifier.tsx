import { createFileRoute, redirect, useLoaderData } from '@tanstack/react-router'
import { IssueDetail, loadIssue } from '@/components/issues/issue-detail'
import { RunPanel } from '@/components/tests/run-panel'

export const Route = createFileRoute('/_app/$workspace/_main/issue/$identifier')({
  preload: false, // long lists and boards: hovering cards mustn't fetch an issue each
  validateSearch: (search): { run?: string } => (typeof search.run === 'string' && search.run ? { run: search.run } : {}),
  loader: async (ctx) => {
    const data = await loadIssue(ctx, ctx.params.workspace, ctx.params.identifier)
    // An old project key or lowercase ID still resolves; show the canonical URL.
    if (data.issue.identifier !== ctx.params.identifier) {
      throw redirect({ to: '/$workspace/issue/$identifier', params: { workspace: ctx.params.workspace, identifier: data.issue.identifier }, search: (prev) => prev, replace: true })
    }
    return data
  },
  component: IssuePage,
})

function IssuePage() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const { user } = Route.useRouteContext()
  const data = Route.useLoaderData()
  const { run } = Route.useSearch()
  const navigate = Route.useNavigate()
  return (
    <main className="w-full px-6 py-10">
      <IssueDetail workspace={workspace} user={user} data={data} />
      {run ? <RunPanel key={run} workspace={workspace} identifier={data.issue.identifier} runId={run} onClose={() => navigate({ search: {} })} /> : null}
    </main>
  )
}
