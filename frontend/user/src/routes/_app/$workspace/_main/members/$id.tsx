import { createFileRoute, notFound } from '@tanstack/react-router'
import { Avatar } from '@tether/shared/avatar'
import { ApiError, avatarSrc, memberAnalyticsPath, type MemberAnalytics } from '@/api'
import { loaderApi } from '@/realtime'
import { MemberCards, useRefetchEvery } from '@/components/analytics/member-analytics'

export const Route = createFileRoute('/_app/$workspace/_main/members/$id')({
  loader: (ctx) =>
    loaderApi<MemberAnalytics>(ctx, memberAnalyticsPath(ctx.params.workspace, ctx.params.id)).then(
      (data) => ({ data, denied: null }),
      (e) => {
        if (e instanceof ApiError && e.status === 404) throw notFound()
        if (e instanceof ApiError && e.status === 403) return { data: null, denied: e.error }
        throw e
      },
    ),
  component: MemberPage,
})

function MemberPage() {
  const { workspace } = Route.useParams()
  const { data, denied } = Route.useLoaderData()
  useRefetchEvery(Route.id)

  return (
    <main className="grid gap-6 w-full px-6 py-10" style={{ alignContent: 'start' }}>
      {data ? (
        <>
          <header className="flex items-center gap-4">
            <Avatar name={data.member.name} size={48} src={avatarSrc(data.member.id)} />
            <div className="grid" style={{ minWidth: 0 }}>
              <h1 className="text-h2 ellipsis">{data.member.name}</h1>
              <span className="text-caption text-secondary">{data.member.role.name}</span>
            </div>
          </header>
          <MemberCards workspace={workspace} data={data} />
        </>
      ) : (
        <>
          <h1 className="text-h2">Member analytics</h1>
          <p className="text-body-sm text-secondary m-0">{denied}</p>
        </>
      )}
    </main>
  )
}
