import { useEffect, useState } from 'react'
import { Link, createFileRoute, useRouter } from '@tanstack/react-router'
import { shortDate } from '@taskwell/shared/time'
import { api, billingPath, useBeta, type Billing } from '@/api'
import { planName } from '@/pricing/tiers'

// Paddle's checkout returns here; the webhook may land a few seconds later, so the page polls until the plan shows.
export const Route = createFileRoute('/_app/welcome')({
  loader: () => api<Billing>(billingPath),
  component: Welcome,
})

function Welcome() {
  const billing = Route.useLoaderData()
  const router = useRouter()
  const beta = useBeta()
  const ready = billing.plan !== 'free'
  const [waiting, setWaiting] = useState(true)

  useEffect(() => {
    if (ready) return
    let n = 0
    const id = setInterval(() => {
      router.invalidate()
      if (++n === 10) {
        clearInterval(id)
        setWaiting(false)
      }
    }, 3000)
    return () => clearInterval(id)
  }, [ready, router])

  const ends = billing.status === 'trialing' ? (billing.trial_ends_at ?? billing.renews_at) : null
  const trial = !ends
    ? ''
    : billing.needs_payment_method
      ? ` Your free trial ends on ${shortDate(ends)}. Add a payment method in billing settings before then to keep it.`
      : ` Your free trial ends on ${shortDate(ends)}.`
  return (
    <main className="grid gap-4 w-full px-8 py-10" style={{ maxWidth: 640, alignContent: 'start' }}>
      <h1 className="text-h2">{ready ? (ends ? 'Your trial has started' : "You're subscribed") : waiting ? 'Setting up your plan' : 'This is taking longer than usual'}</h1>
      <p role="status" className="text-body-sm text-secondary m-0">
        {ready
          ? `You're on ${planName(billing.plan)}.${trial}`
          : waiting
            ? 'Thanks for subscribing. Your plan shows here in a few seconds.'
            : "Your plan hasn't shown up yet. Refresh this page in a minute; if it still isn't there, contact us and we'll sort it out."}
      </p>
      <div className="flex flex-wrap gap-3">
        <Link to="/signin" className="button button-primary">
          Go to dashboard
        </Link>
        {beta ? null : (
          <Link to="/billing" className="button button-secondary">
            Billing settings
          </Link>
        )}
      </div>
    </main>
  )
}
