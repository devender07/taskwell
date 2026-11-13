import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { can, storagePath, type StorageUsage } from '@/api'
import { loaderApi } from '@/realtime'
import { formatBytes } from '@/components/files/bytes'
import { DeleteWorkspace } from '@/components/workspace/delete-workspace'
import { ExportWorkspace } from '@/components/workspace/export-workspace'
import { InviteForm } from '@/components/workspace/members'
import { WorkspaceSettingsForm } from '@/components/workspace/settings-form'
import '@/components/home/home.css'

export const Route = createFileRoute('/_app/$workspace/_main/settings/workspace')({
  loader: (ctx) => loaderApi<StorageUsage>(ctx, storagePath(ctx.params.workspace)),
  component: WorkspaceSettings,
})

function WorkspaceSettings() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const { used_bytes: used, quota_bytes: quota } = Route.useLoaderData()
  const usage = quota === null ? `${formatBytes(used)} used` : `${formatBytes(used)} of ${formatBytes(quota)} used`
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
      <section className="grid gap-3 p-6 bg-surface border rounded-lg" aria-labelledby="storage-title">
        <h2 id="storage-title" className="text-label">Storage</h2>
        <p className="text-body-sm text-secondary m-0">{usage}</p>
        {quota === null ? null : (
          <span role="progressbar" aria-label="Storage used" aria-valuemin={0} aria-valuemax={quota} aria-valuenow={used} aria-valuetext={usage} className="home-bar">
            <span style={{ width: quota ? `${Math.min(100, (used / quota) * 100)}%` : 0 }} />
          </span>
        )}
      </section>
      {can(workspace, 'members.invite') ? (
        <section className="grid gap-4 p-6 bg-surface border rounded-lg">
          <InviteForm key={workspace.id} workspace={workspace} />
        </section>
      ) : null}
      {can(workspace, 'workspace.export') ? <ExportWorkspace workspace={workspace} /> : null}
      {can(workspace, 'workspace.delete') ? <DeleteWorkspace workspace={workspace} /> : null}
    </main>
  )
}
