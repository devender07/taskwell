import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import type { Label, Member, Status } from '@/api'
import { loaderApi } from '@/realtime'
import { IssueView } from '@/components/issues/issue-view'
import { hasFilters, issuePageQuery, validateViewSearch, type IssuePage } from '@/components/issues/view-search'
import { workspacePath } from '@/api'

export const Route = createFileRoute('/_app/$workspace/_main/my-issues')({
  validateSearch: validateViewSearch,
  loaderDeps: ({ search }) => ({ query: issuePageQuery(search, true) }),
  loader: async (ctx) => {
    const { params } = ctx
    const path = `${workspacePath(params.workspace)}`
    const source = `${path}/my-issues`
    const [page, statuses, labels, { members }] = await Promise.all([
      loaderApi<IssuePage>(ctx, `${source}?${ctx.deps.query}`),
      loaderApi<Status[]>(ctx, `${path}/statuses`),
      loaderApi<Label[]>(ctx, `${path}/labels`),
      loaderApi<{ members: Member[] }>(ctx, `${path}/members`),
    ])
    return { page, source, statuses, labels, members }
  },
  component: MyIssues,
})

function MyIssues() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const { page, source, statuses, labels, members } = Route.useLoaderData()
  const { user } = Route.useRouteContext()
  const search = Route.useSearch()

  return (
    <main className="grid gap-6 w-full px-6 py-10" style={{ alignContent: 'start' }}>
      <h1 className="text-h2">My issues</h1>
      {page.total === 0 && !hasFilters(search, true) ? (
        <p className="text-body-sm text-secondary p-6 bg-surface border rounded-lg text-center">Nothing assigned to you.</p>
      ) : (
        <IssueView workspace={workspace} page={page} source={source} statuses={statuses} labels={labels} members={members} userId={user.id} search={search} mine />
      )}
    </main>
  )
}
