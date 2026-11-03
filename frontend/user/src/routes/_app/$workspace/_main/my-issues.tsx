import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { Tabs } from '@tether/shared/tabs'
import type { Label, Member, Status } from '@/api'
import { loaderApi } from '@/realtime'
import { IssueView } from '@/components/issues/issue-view'
import { hasFilters, issuePageQuery, validateViewSearch, type IssuePage, type ViewSearch } from '@/components/issues/view-search'
import { workspacePath } from '@/api'

type Search = ViewSearch & { watching?: true }

const tabs = [
  { value: 'assigned', label: 'Assigned' },
  { value: 'watching', label: 'Watching' },
] as const

export const Route = createFileRoute('/_app/$workspace/_main/my-issues')({
  validateSearch: (search): Search => ({ ...validateViewSearch(search), ...(search.watching === true ? { watching: true as const } : {}) }),
  loaderDeps: ({ search }) => ({ watching: !!search.watching, query: issuePageQuery(search, !search.watching) }),
  loader: async (ctx) => {
    const { params } = ctx
    const path = `${workspacePath(params.workspace)}`
    const source = `${path}/${ctx.deps.watching ? 'watching' : 'my-issues'}`
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
  const navigate = Route.useNavigate()
  const watching = !!search.watching

  return (
    <main className="grid gap-6 w-full px-6 py-10" style={{ alignContent: 'start' }}>
      <h1 className="text-h2">My issues</h1>
      <Tabs
        aria-label="Issues shown"
        className="w-fit"
        items={[...tabs]}
        value={watching ? 'watching' : 'assigned'}
        onChange={(v) => navigate({ search: v === 'watching' ? { watching: true } : {} })}
      />
      {page.total === 0 && !hasFilters(search, !watching) ? (
        <p className="text-body-sm text-secondary p-6 bg-surface border rounded-lg text-center">
          {watching ? "You're not watching any issues." : 'Nothing assigned to you.'}
        </p>
      ) : (
        <IssueView workspace={workspace} page={page} source={source} statuses={statuses} labels={labels} members={members} userId={user.id} search={search} mine={!watching} />
      )}
    </main>
  )
}
