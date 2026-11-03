import { useNavigate } from '@tanstack/react-router'
import { useActionState, useState } from 'react'
import { Button } from '@tether/shared/button'
import { Dialog } from '@tether/shared/dialog'
import { Input } from '@tether/shared/input'
import { api, homePath, toFormErrors, type Workspace, workspacePath } from '@/api'
import { FormError } from '@/components/auth/auth-card'

// Danger zone: deleting needs the workspace name typed back, then sends the user to their next workspace.
export function DeleteWorkspace({ workspace }: { workspace: Workspace }) {
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)
  const [typed, setTyped] = useState('')

  const [error, remove, pending] = useActionState(async () => {
    try {
      await api(`${workspacePath(workspace.slug)}`, undefined, 'DELETE')
    } catch (e) {
      return toFormErrors(e).error
    }
    await navigate({ href: await homePath(), replace: true }) // the most recent remaining workspace, or creating one
    return null
  }, null)

  return (
    <section className="grid gap-4 p-6 border rounded-lg" style={{ borderColor: 'rgba(249, 24, 128, 0.4)' }}>
      <div className="grid gap-1">
        <h2 className="text-label">Delete workspace</h2>
        <p className="text-body-sm text-secondary">
          Permanently delete {workspace.name} for everyone in it. This can't be undone.
        </p>
      </div>
      <Button variant="danger" style={{ justifySelf: 'start' }} onClick={() => setOpen(true)}>
        Delete workspace
      </Button>

      <Dialog
        open={open}
        onClose={() => {
          setOpen(false)
          setTyped('')
        }}
        title="Delete workspace?"
        description={
          <>
            This permanently deletes <strong className="text-primary font-510">{workspace.name}</strong> and removes
            everyone from it.
          </>
        }
      >
        <form action={remove} className="grid gap-4">
          <Input
            label={`Type ${workspace.name} to confirm`}
            autoComplete="off"
            spellCheck={false}
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
          />
          <FormError>{error}</FormError>
          <Button type="submit" variant="danger" className="w-full" disabled={typed !== workspace.name || pending}>
            {pending ? 'Deleting…' : 'Delete workspace'}
          </Button>
        </form>
      </Dialog>
    </section>
  )
}
