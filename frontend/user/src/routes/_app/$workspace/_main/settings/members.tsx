import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { can, type Invite, type Member, type Role } from '@/api'
import { loaderApi, trackPath } from '@/realtime'
import { InviteForm, MemberList, PendingInvites } from '@/components/workspace/members'

export const Route = createFileRoute('/_app/$workspace/_main/settings/members')({
  loader: async (ctx) => {
    const path = `/workspaces/${encodeURIComponent(ctx.params.workspace)}`
    trackPath(ctx, `${path}/invites`) // the members response lists invites too
    const [{ members, invites }, roles] = await Promise.all([
      loaderApi<{ members: Member[]; invites: Invite[] }>(ctx, `${path}/members`),
      loaderApi<Role[]>(ctx, `${path}/roles`),
    ])
    return { members, invites, roles }
  },
  component: MembersSettings,
})

function MembersSettings() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const { user } = Route.useRouteContext()
  const { members, invites, roles } = Route.useLoaderData()
  const invite = can(workspace, 'members.invite')
  return (
    <main className="grid gap-6 w-full px-8 py-10" style={{ alignContent: 'start' }}>
      <header className="grid gap-2">
        <h1 className="text-h2">Members</h1>
        <p className="text-body-sm text-secondary">People in {workspace.name}.</p>
      </header>
      {invite ? (
        <section className="grid gap-4 p-6 bg-surface border rounded-lg">
          <InviteForm key={workspace.id} workspace={workspace} roles={roles} />
        </section>
      ) : null}
      <MemberList workspace={workspace} roles={roles} members={members} userId={user.id} />
      {invite && invites.length > 0 ? <PendingInvites slug={workspace.slug} invites={invites} /> : null}
    </main>
  )
}
