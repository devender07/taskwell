import { Link, createFileRoute, redirect, useLoaderData } from '@tanstack/react-router'
import { IssueDetail, loadIssue } from '@/components/issues/issue-detail'

export const Route = createFileRoute('/_app/$workspace/_main/inbox/$identifier')({
  loader: async (ctx) => {
    const data = await loadIssue(ctx, ctx.params.workspace, ctx.params.identifier)
    if (data.issue.identifier !== ctx.params.identifier) {
      throw redirect({
        to: '/$workspace/inbox/$identifier',
        params: { workspace: ctx.params.workspace, identifier: data.issue.identifier },
        search: ctx.location.search,
        replace: true,
      })
    }
    return data
  },
  component: InboxIssue,
})

function InboxIssue() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const { user } = Route.useRouteContext()
  const data = Route.useLoaderData()
  const search = Route.useSearch()
  return (
    <IssueDetail
      key={data.issue.id}
      workspace={workspace}
      user={user}
      data={data}
      actions={
        <div className="flex items-center gap-4 flex-noshrink">
          <Link to="/$workspace/inbox" params={{ workspace: workspace.slug }} search={search} className="inbox-back text-caption text-secondary">
            ← Inbox
          </Link>
          <Link to="/$workspace/issue/$identifier" params={{ workspace: workspace.slug, identifier: data.issue.identifier }} className="text-caption text-secondary">
            Open issue
          </Link>
        </div>
      }
    />
  )
}
