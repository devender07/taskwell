import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { api, type Issue, type Label, type Member, type Status } from '@/api'
import { IssueView } from '@/components/issues/issue-view'
import { validateViewSearch } from '@/components/issues/view-search'

export const Route = createFileRoute('/_app/$workspace/_main/my-issues')({
  validateSearch: validateViewSearch,
  loader: async ({ params }) => {
    const path = `/workspaces/${encodeURIComponent(params.workspace)}`
    const [issues, statuses, labels, { members }] = await Promise.all([
      api<Issue[]>(`${path}/my-issues`),
      api<Status[]>(`${path}/statuses`),
      api<Label[]>(`${path}/labels`),
      api<{ members: Member[] }>(`${path}/members`),
    ])
    return { issues, statuses, labels, members }
  },
  component: MyIssues,
})

function MyIssues() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const { issues, statuses, labels, members } = Route.useLoaderData()
  const { user } = Route.useRouteContext()
  const search = Route.useSearch()

  return (
    <main className="grid gap-6 w-full px-6 py-10" style={{ alignContent: 'start' }}>
      <h1 className="text-h2">My issues</h1>
      {issues.length === 0 ? (
        <p className="text-body-sm text-secondary p-6 bg-surface border rounded-lg text-center">Nothing assigned to you.</p>
      ) : (
        <IssueView workspace={workspace} issues={issues} statuses={statuses} labels={labels} members={members} userId={user.id} search={search} mine />
      )}
    </main>
  )
}
