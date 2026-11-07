import { useActionState } from 'react'
import { apiUrl } from '@taskwell/shared/api-url'
import { Button } from '@taskwell/shared/button'
import { startExport, toFormErrors, type Workspace } from '@/api'
import { FormError } from '@/components/auth/auth-card'

export function ExportWorkspace({ workspace }: { workspace: Workspace }) {
  const [error, start, pending] = useActionState(async () => {
    try {
      const { url } = await startExport(workspace.slug)
      location.assign(apiUrl(url))
      return null
    } catch (e) {
      return toFormErrors(e).error
    }
  }, null)

  return (
    <section className="grid gap-4 p-6 bg-surface border rounded-lg">
      <div className="grid gap-1">
        <h2 className="text-label">Export data</h2>
        <p className="text-body-sm text-secondary">
          Download this workspace's projects, issues, comments and files as a zip of JSON and CSV. Private projects
          you're not in are left out.
        </p>
      </div>
      <form action={start} className="grid gap-2">
        <Button type="submit" variant="secondary" style={{ justifySelf: 'start' }} disabled={pending}>
          {pending ? 'Preparing…' : 'Export workspace'}
        </Button>
        <FormError>{error}</FormError>
      </form>
    </section>
  )
}
