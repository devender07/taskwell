import { useState, useTransition } from 'react'
import { Button } from '@taskwell/shared/button'
import { openBillingPortal, toFormErrors, type PlanId } from '@/api'
import { planName } from '@/pricing/tiers'

export function PastDueBanner({ plan }: { plan: PlanId }) {
  const [error, setError] = useState<string | null>(null)
  const [pending, startTransition] = useTransition()
  const update = () =>
    startTransition(async () => {
      setError(null)
      try {
        location.assign((await openBillingPortal()).url)
      } catch (e) {
        setError(toFormErrors(e).error)
      }
    })

  return (
    <div role="status" className="flex flex-wrap items-center gap-3 px-6 py-3 bg-surface" style={{ borderBottom: '1px solid var(--accent)' }}>
      <p className="text-body-sm m-0 flex-grow">
        <span className="text-accent font-510">Your last payment failed.</span> Update your payment method to keep {planName(plan)}.
      </p>
      {error ? <p role="alert" className="text-caption text-accent m-0">{error}</p> : null}
      <Button variant="primary" disabled={pending} onClick={update}>
        Update payment method
      </Button>
    </div>
  )
}
