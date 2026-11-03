import { createFileRoute, redirect } from '@tanstack/react-router'
import { Dialog } from '@tether/shared/dialog'
import { CreateWorkspaceForm } from '@/components/workspace/create-form'
import { readNotice } from '@/realtime'

// Where a user with no workspace lands (their default one failed to be created, or they left their last): a create
// dialog they can't dismiss. Anyone with a workspace goes to their most recent one.
export const Route = createFileRoute('/_app/new-workspace')({
  beforeLoad: ({ context }) => {
    const last = context.workspaces[0]
    if (last) throw redirect({ to: '/$workspace', params: { workspace: last.slug } })
  },
  component: () => (
    <Dialog open onClose={() => {}} dismissible={false} title="Create a workspace" description="You need a workspace to get started. It holds your team's projects and issues.">
      {readNotice() ? <p role="status" className="text-caption text-secondary m-0">{readNotice()}</p> : null}
      <CreateWorkspaceForm />
    </Dialog>
  ),
})
