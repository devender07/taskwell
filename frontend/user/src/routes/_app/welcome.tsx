import { useEffect } from 'react'
import { Link, createFileRoute, useRouter } from '@tanstack/react-router'
import { shortDate } from '@taskwell/shared/time'
import { api, billingPath, type Billing } from '@/api'
import { planName } from '@/pricing/tiers'

// Paddle's checkout returns here; the webhook may land a few seconds later, so the page polls until the plan shows.
export const Route = createFileRoute('/_app/welcome')({
  loader: () => api<Billing>(billingPath),
  component: Welcome,
})

function Welcome() {
  const billing = Route.useLoaderData()
  const router = useRouter()
  const ready = billing.plan !== 'free'

  useEffect(() => {
    if (ready) return
    let n = 0
    const id = setInterval(() => {
      router.invalidate()
      if (++n === 10) clearInterval(id)
    }, 3000)
    return () => clearInterval(id)
  }, [ready, router])

  const trial = billing.status === 'trialing' && billing.renews_at
  return (
    <main className="grid gap-4 w-full px-8 py-10" style={{ maxWidth: 640, alignContent: 'start' }}>
      <h1 className="text-h2">{ready ? "You're subscribed" : 'Setting up your plan'}</h1>
      <p role="status" className="text-body-sm text-secondary m-0">
        {ready
          ? `You're on ${planName(billing.plan)}.${trial ? ` Your free trial ends on ${shortDate(billing.renews_at!)}.` : ''}`
          : 'Thanks for subscribing. Your plan shows here in a few seconds.'}
      </p>
      <Link to="/signin" className="button button-primary" style={{ justifySelf: 'start' }}>
        Go to dashboard
      </Link>
    </main>
  )
}
