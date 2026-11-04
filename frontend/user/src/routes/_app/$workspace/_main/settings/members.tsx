import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { can, type Project, type Role, workspacePath } from '@/api'
import { loaderApi, trackPath } from '@/realtime'
import { InviteForm, MemberList, PendingInvites } from '@/components/workspace/members'
import { type MemberPage, memberPagePath } from '@/components/workspace/member-pages'

export const Route = createFileRoute('/_app/$workspace/_main/settings/members')({
  validateSearch: (search): { q?: string } => (typeof search.q === 'string' && search.q ? { q: search.q } : {}),
  loaderDeps: ({ search }) => ({ q: search.q?.trim() ?? '' }),
  loader: async (ctx) => {
    const path = `${workspacePath(ctx.params.workspace)}`
    trackPath(ctx, `${path}/invites`) // the members response lists invites too
    const [page, roles, projects] = await Promise.all([
      loaderApi<MemberPage>(ctx, memberPagePath(ctx.params.workspace, ctx.deps.q)),
      loaderApi<Role[]>(ctx, `${path}/roles`),
      loaderApi<Project[]>(ctx, `${path}/projects`), // for guests: which projects they're invited to and in
    ])
    return { page, roles, projects }
  },
  component: MembersSettings,
})

function MembersSettings() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const { user } = Route.useRouteContext()
  const { page, roles, projects } = Route.useLoaderData()
  const { q = '' } = Route.useSearch()
  const navigate = Route.useNavigate()
  const invite = can(workspace, 'members.invite')
  return (
    <main className="grid gap-6 w-full px-8 py-10" style={{ alignContent: 'start' }}>
      <header className="grid gap-2">
        <h1 className="text-h2">Members</h1>
        <p className="text-body-sm text-secondary">People in {workspace.name}.</p>
      </header>
      {invite ? (
        <section className="grid gap-4 p-6 bg-surface border rounded-lg">
          <InviteForm key={workspace.id} workspace={workspace} roles={roles} projects={projects} />
        </section>
      ) : null}
      <MemberList
        workspace={workspace}
        roles={roles}
        projects={projects}
        page={page}
        userId={user.id}
        query={q}
        onQuery={(next) => navigate({ search: next ? { q: next } : {}, replace: true })}
      />
      {invite && page.invites.length > 0 ? <PendingInvites slug={workspace.slug} invites={page.invites} /> : null}
    </main>
  )
}
