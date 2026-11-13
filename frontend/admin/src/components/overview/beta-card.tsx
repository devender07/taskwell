import { useRouter } from '@tanstack/react-router'
import { useActionState } from 'react'
import { Button } from '@taskwell/shared/button'
import { api, toFormErrors } from '@/api'

export function BetaCard({ on }: { on: boolean }) {
  const router = useRouter()
  const [error, toggle, pending] = useActionState(async () => {
    const form = new FormData()
    form.set('value', on ? 'false' : 'true')
    try {
      await api('/config/beta_mode', form)
    } catch (e) {
      const { error, errors } = toFormErrors(e)
      return error ?? errors.value ?? null
    }
    await router.invalidate()
    return null
  }, null)

  return (
    <form action={toggle} className="flex flex-wrap items-center justify-between gap-4 p-5 bg-surface border rounded-lg" aria-label="Beta mode">
      <div className="grid gap-1" style={{ maxWidth: 640 }}>
        <h2 className="text-label m-0">
          Beta mode <span className={on ? 'text-success' : 'text-secondary'}>· {on ? 'On' : 'Off'}</span>
        </h2>
        <p className="text-caption text-secondary m-0">
          While it's on, pricing, billing and upgrade prompts are hidden, everyone gets the Free plan's limits, and the beta notice replaces the
          privacy notice and terms.
        </p>
        {error ? <p role="alert" className="text-caption text-accent m-0">{error}</p> : null}
      </div>
      <Button type="submit" disabled={pending}>
        {pending ? 'Saving…' : on ? 'Turn off beta mode' : 'Turn on beta mode'}
      </Button>
    </form>
  )
}
