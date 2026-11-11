import { useState, useTransition } from 'react'
import { Button } from '@taskwell/shared/button'
import { longDate } from '@taskwell/shared/time'
import { startPaymentMethod, toFormErrors, type Billing } from '@/api'
import { openPaymentMethod } from '@/paddle'
import { planName } from '@/pricing/tiers'

// Opens Paddle's checkout for adding a payment method to the account's subscription; nothing is charged.
export function AddPaymentMethod({ onError }: { onError: (message: string | null) => void }) {
  const [pending, startTransition] = useTransition()
  const add = () =>
    startTransition(async () => {
      onError(null)
      try {
        await openPaymentMethod((await startPaymentMethod()).transaction_id, `${location.origin}/billing`)
      } catch (e) {
        onError(toFormErrors(e).error)
      }
    })
  return (
    <Button variant="primary" disabled={pending} onClick={add}>
      Add payment method
    </Button>
  )
}

// A trial started without a payment method ends in Free unless one is added.
export function TrialBanner({ billing }: { billing: Billing }) {
  const [error, setError] = useState<string | null>(null)
  return (
    <div role="status" className="flex flex-wrap items-center gap-3 px-6 py-3 bg-surface" style={{ borderBottom: '1px solid var(--border)' }}>
      <p className="text-body-sm m-0 flex-grow">
        <span className="font-510">Your {planName(billing.plan)} trial ends{billing.trial_ends_at ? ` on ${longDate(billing.trial_ends_at)}` : ' soon'}.</span> Add a payment
        method to keep it. You won't be charged until the trial ends.
      </p>
      {error ? <p role="alert" className="text-caption text-accent m-0">{error}</p> : null}
      <AddPaymentMethod onError={setError} />
    </div>
  )
}
