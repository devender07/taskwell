import { useRouter } from '@tanstack/react-router'
import { startTransition, useActionState } from 'react'
import { Checkbox } from '@taskwell/shared/checkbox'
import { Input } from '@taskwell/shared/input'
import { api, feedbackFormPath, toFormErrors, type FeedbackForm } from '@/api'
import { reloadPage } from '@/realtime'
import { FormError } from '@/components/auth/auth-card'
import { ConfirmButton } from '@/components/tests/tests'
import { CopyButton } from './integrations'

const save = (workspace: string, fields: Record<string, string>) => {
  const form = new FormData()
  for (const [k, v] of Object.entries(fields)) form.set(k, v)
  return api<FeedbackForm>(feedbackFormPath(workspace), form)
}

export function FeedbackFormCard({ workspace, form }: { workspace: string; form: FeedbackForm }) {
  const router = useRouter()
  const link = form.token ? `${location.origin}/f/${form.token}` : null

  const [error, toggle, pending] = useActionState(async (_prev: string | null, on: boolean) => {
    try {
      await save(workspace, { enabled: String(on) })
    } catch (e) {
      return toFormErrors(e).error
    }
    await reloadPage(router)
    return null
  }, null)

  return (
    <section className="grid gap-4 p-6 bg-surface border rounded-lg">
      <Checkbox label="Let customers send feedback with a link" checked={pending !== form.enabled} disabled={pending} onChange={(e) => startTransition(() => toggle(e.target.checked))} />
      <FormError>{error}</FormError>
      {form.enabled && link ? (
        <>
          <div className="flex items-end gap-3">
            <div className="flex-grow" style={{ minWidth: 0 }}>
              <Input label="Form link" readOnly value={link} onFocus={(e) => e.currentTarget.select()} />
            </div>
            <CopyButton text={link} label="Copy form link" />
          </div>
          <p className="text-caption text-secondary m-0">Anyone with the link can send feedback. It lands in Feedback as new.</p>
          <div>
            <ConfirmButton
              label="Create a new link"
              title="Create a new link?"
              description="The old link stops working."
              confirm="Create a new link"
              pending="Creating…"
              onConfirm={async () => {
                await save(workspace, { rotate: 'true' })
                await reloadPage(router)
              }}
            />
          </div>
        </>
      ) : null}
    </section>
  )
}
