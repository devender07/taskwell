import { createFileRoute, redirect } from '@tanstack/react-router'
import { Dialog } from '@tether/shared/dialog'
import { CreateWorkspaceForm } from '@/components/workspace/create-form'

// Opens the most recently used workspace. A user with none (their default workspace failed to be created)
// gets a create dialog they can't dismiss.
export const Route = createFileRoute('/_app/')({
  beforeLoad: ({ context }) => {
    const last = context.workspaces[0]
    if (last) throw redirect({ to: '/$workspace', params: { workspace: last.slug } })
  },
  component: () => (
    <Dialog open onClose={() => {}} dismissible={false} title="Create a workspace" description="You need a workspace to get started. It holds your team's projects and issues.">
      <CreateWorkspaceForm />
    </Dialog>
  ),
})
