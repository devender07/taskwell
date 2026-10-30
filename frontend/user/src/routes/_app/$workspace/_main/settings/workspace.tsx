import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { DeleteWorkspace } from '@/components/workspace/delete-workspace'
import { WorkspaceSettingsForm } from '@/components/workspace/settings-form'

export const Route = createFileRoute('/_app/$workspace/_main/settings/workspace')({
  component: WorkspaceSettings,
})

function WorkspaceSettings() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  return (
    <main className="grid gap-6 w-full px-6 py-10" style={{ maxWidth: 720, alignContent: 'start' }}>
      <header className="grid gap-2">
        <h1 className="text-h2">Workspace</h1>
        <p className="text-body-sm text-secondary">Your workspace's name and URL.</p>
      </header>
      <section className="grid gap-4 p-6 bg-surface border rounded-lg">
        {/* Keyed by id so switching workspaces resets the form; a rename of this one keeps it (and "Saved."). */}
        <WorkspaceSettingsForm key={workspace.id} workspace={workspace} />
      </section>
      {workspace.role === 'admin' ? <DeleteWorkspace workspace={workspace} /> : null}
    </main>
  )
}
