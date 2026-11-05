import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { can } from '@/api'
import { DeleteWorkspace } from '@/components/workspace/delete-workspace'
import { InviteForm } from '@/components/workspace/members'
import { WorkspaceSettingsForm } from '@/components/workspace/settings-form'

export const Route = createFileRoute('/_app/$workspace/_main/settings/workspace')({
  component: WorkspaceSettings,
})

function WorkspaceSettings() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  return (
    <main className="grid gap-6 w-full px-8 py-10" style={{ alignContent: 'start' }}>
      <header className="page-top grid gap-2">
        <h1 className="text-h2">Workspace</h1>
        <p className="text-body-sm text-secondary">Your workspace's name and URL.</p>
      </header>
      <section className="grid gap-4 p-6 bg-surface border rounded-lg">
        {/* Keyed by id so switching workspaces resets the form; a rename of this one keeps it (and "Saved."). */}
        <WorkspaceSettingsForm key={workspace.id} workspace={workspace} />
      </section>
      {can(workspace, 'members.invite') ? (
        <section className="grid gap-4 p-6 bg-surface border rounded-lg">
          <InviteForm key={workspace.id} workspace={workspace} />
        </section>
      ) : null}
      {can(workspace, 'workspace.delete') ? <DeleteWorkspace workspace={workspace} /> : null}
    </main>
  )
}
