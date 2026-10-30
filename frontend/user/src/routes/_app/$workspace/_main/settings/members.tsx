import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { api, can, type Invite, type Member, type Role } from '@/api'
import { InviteForm, MemberList, PendingInvites } from '@/components/workspace/members'

export const Route = createFileRoute('/_app/$workspace/_main/settings/members')({
  loader: async ({ params }) => {
    const path = `/workspaces/${encodeURIComponent(params.workspace)}`
    const [{ members, invites }, roles] = await Promise.all([
      api<{ members: Member[]; invites: Invite[] }>(`${path}/members`),
      api<Role[]>(`${path}/roles`),
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
    <main className="grid gap-6 w-full px-6 py-10" style={{ maxWidth: 720, alignContent: 'start' }}>
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
