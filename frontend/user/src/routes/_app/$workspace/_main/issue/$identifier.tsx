import { createFileRoute, redirect, useLoaderData } from '@tanstack/react-router'
import { IssueDetail, loadIssue } from '@/components/issues/issue-detail'

export const Route = createFileRoute('/_app/$workspace/_main/issue/$identifier')({
  loader: async (ctx) => {
    const data = await loadIssue(ctx, ctx.params.workspace, ctx.params.identifier)
    // An old project key or lowercase ID still resolves; show the canonical URL.
    if (data.issue.identifier !== ctx.params.identifier) {
      throw redirect({ to: '/$workspace/issue/$identifier', params: { workspace: ctx.params.workspace, identifier: data.issue.identifier }, replace: true })
    }
    return data
  },
  component: IssuePage,
})

function IssuePage() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const { user } = Route.useRouteContext()
  const data = Route.useLoaderData()
  return (
    <main className="w-full px-6 py-10">
      <IssueDetail workspace={workspace} user={user} data={data} />
    </main>
  )
}
