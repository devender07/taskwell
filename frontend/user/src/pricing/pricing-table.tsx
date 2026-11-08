import { useEffect, useState, useTransition } from 'react'
import { Link } from '@tanstack/react-router'
import { Button } from '@taskwell/shared/button'
import { Dialog } from '@taskwell/shared/dialog'
import { PlanCard } from '@taskwell/shared/plan-card'
import { Tabs } from '@taskwell/shared/tabs'
import { ApiError, changePlan, startCheckout, toFormErrors, type Billing } from '@/api'
import { FormError } from '@/components/auth/auth-card'
import { openCheckout, previewPrices } from '@/paddle'
import { PRICE_IDS, TIERS, type Interval, type Tier } from '@/pricing/tiers'

const INTERVALS: { value: Interval; label: string }[] = [
  { value: 'month', label: 'Monthly' },
  { value: 'year', label: 'Yearly' },
]

export const SUBSCRIBED = ['active', 'trialing', 'past_due']

export const validateInterval = (v: unknown): Interval => (v === 'year' ? 'year' : 'month')

type Props = {
  interval: Interval
  onInterval: (i: Interval) => void
  billing: Billing | null // null when signed out
  onChanged?: () => Promise<void> | void
}

export function PricingTable({ interval, onInterval, billing, onChanged }: Props) {
  const [prices, setPrices] = useState<Record<string, string> | null>(null)
  const [priceError, setPriceError] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [pending, startTransition] = useTransition()
  const [switchTo, setSwitchTo] = useState<Tier | null>(null)

  useEffect(() => {
    let live = true
    previewPrices(PRICE_IDS).then(
      (p) => live && setPrices(p),
      (e) => {
        console.error(e)
        if (live) setPriceError(true)
      },
    )
    return () => {
      live = false
    }
  }, [])

  const run = (task: () => Promise<void>) =>
    startTransition(async () => {
      setError(null)
      try {
        await task()
      } catch (e) {
        if (!(e instanceof ApiError)) console.error(e)
        setError(toFormErrors(e).error)
      }
    })

  const subscribe = (priceId: string) =>
    run(async () => {
      const c = await startCheckout(priceId)
      await openCheckout(priceId, c.email, c.custom_data, `${location.origin}/welcome`)
    })

  const confirmSwitch = (priceId: string) =>
    run(async () => {
      await changePlan(priceId)
      setSwitchTo(null)
      await onChanged?.()
    })

  const subscribed = SUBSCRIBED.includes(billing?.status ?? '')
  const actions = billing ? billing.enabled && !billing.comped : true
  const period = interval === 'month' ? '/ month' : '/ year'
  const trial = !billing?.trial_used

  const cta = (t: Tier) => {
    const priceId = (trial ? t.priceId : t.noTrial)[interval]
    const variant = t.featured ? 'secondary' : 'primary'
    if (!billing) {
      return (
        <Link to="/signup" search={{ redirect: `/pricing?interval=${interval}` }} className={`button button-${variant}`}>
          Start free trial
        </Link>
      )
    }
    if (subscribed && billing.plan === t.id && billing.interval === interval) return <p className="plan-current">Current plan</p>
    if (!actions) return null
    return (
      <Button variant={variant} disabled={pending} onClick={() => (subscribed ? setSwitchTo(t) : subscribe(priceId))}>
        {subscribed ? 'Switch plan' : trial ? 'Start free trial' : 'Subscribe'}
      </Button>
    )
  }

  return (
    <div className="grid gap-6">
      <div className="flex">
        <Tabs items={INTERVALS} value={interval} onChange={onInterval} aria-label="Billing interval" />
      </div>
      {billing && !billing.enabled ? <p className="text-body-sm text-secondary m-0">Billing isn't set up yet.</p> : null}
      {priceError ? <p className="text-body-sm text-secondary m-0">Prices couldn't load. Refresh the page to try again.</p> : null}
      <ul className="plan-grid">
        {TIERS.map((t) => (
          <PlanCard
            key={t.id}
            name={t.name}
            tagline={t.description}
            price={prices?.[t.priceId[interval]]}
            period={period}
            note={trial ? '7-day free trial' : undefined}
            featured={t.featured}
            checks={t.features}
          >
            {cta(t)}
          </PlanCard>
        ))}
      </ul>
      {switchTo ? null : <FormError>{error}</FormError>}
      <Dialog
        open={switchTo !== null}
        onClose={() => {
          setSwitchTo(null)
          setError(null)
        }}
        title={`Switch to ${switchTo?.name ?? ''} ${interval === 'month' ? 'monthly' : 'yearly'} now?`}
        description="You'll be charged the prorated difference today."
      >
        <div className="grid gap-4">
          <FormError>{error}</FormError>
          <Button variant="secondary" className="w-full" disabled={pending} onClick={() => switchTo && confirmSwitch(switchTo.noTrial[interval])}>
            {pending ? 'Switching…' : 'Switch plan'}
          </Button>
        </div>
      </Dialog>
    </div>
  )
}
