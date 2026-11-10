import { useEffect, useState, useTransition } from 'react'
import { Link } from '@tanstack/react-router'
import { Button } from '@taskwell/shared/button'
import { Dialog } from '@taskwell/shared/dialog'
import { PlanCard, PlanIncludes } from '@taskwell/shared/plan-card'
import { Tabs } from '@taskwell/shared/tabs'
import { useToast } from '@taskwell/shared/toast'
import { ApiError, changePlan, getFeaturePlans, previewPlanChange, startCheckout, toFormErrors, type Billing, type ChangePreview, type Feature } from '@/api'
import { FormError } from '@/components/auth/auth-card'
import { openCheckout, previewPrices } from '@/paddle'
import { switchLabel, switchSummary } from '@/pricing/switch'
import { FREE, PRICE_IDS, TIERS, type Interval, type Tier } from '@/pricing/tiers'

const INTERVALS: { value: Interval; label: string }[] = [
  { value: 'month', label: 'Monthly' },
  { value: 'year', label: 'Yearly' },
]

export const SUBSCRIBED = ['active', 'trialing', 'past_due']

export const validateInterval = (v: unknown): Interval => (v === 'year' ? 'year' : 'month')

const CORE = ['Projects and issues', 'List and board views', 'Comments and mentions', 'Notifications']
export const FEATURES: [Feature, string, string][] = [
  ['cycles', 'Cycles', 'Plan work in time-boxed cycles and track each one with a burndown.'],
  ['roadmap', 'Roadmap and milestones', 'See every project on a timeline with its milestones and health.'],
  ['notes', 'Notes', 'Write and share docs alongside your issues, edited live together.'],
  ['chat', 'Chat', 'Message your team in channels and direct messages, with an inbox for mentions.'],
  ['files', 'Project files', 'Keep files and folders with each project and attach them to issues.'],
  ['feedback', 'Customer feedback', 'Collect feedback from customers with a public form and turn it into issues.'],
  ['team', 'Team analytics', "See each member's workload, throughput and cycle time."],
  ['templates', 'Templates', 'Start issues from reusable templates with fields filled in.'],
  ['imports', 'CSV and GitHub import', 'Bring issues in from a CSV file or a GitHub repository.'],
  ['ai_testing', 'AI testing', 'Let an AI agent run your test cases in a real browser.'],
  ['api', 'MCP and API', 'Connect Claude and other tools through MCP and the REST API.'],
  ['webhooks', 'Webhooks', 'Send workspace events to your own services as they happen.'],
  ['integrations', 'GitHub and GitLab', 'Link pull requests and merge requests to their issues.'],
]

// Features every plan has go under "Every plan includes"; a card lists the ones only some plans have.
// Before the flags load (or if they can't), every feature counts as on everywhere.
function split(plans: Partial<Record<Feature, string[]>> | null) {
  const on = (f: Feature, tier: string) => !plans?.[f] || plans[f]!.includes(tier)
  const everywhere = FEATURES.filter(([f]) => [FREE, ...TIERS].every((t) => on(f, t.id)))
  return {
    includes: [...CORE, ...everywhere.map(([, label]) => label)],
    extras: (tier: string) => FEATURES.filter(([f]) => on(f, tier) && !everywhere.some(([e]) => e === f)).map(([, label]) => label),
  }
}

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
  const [preview, setPreview] = useState<ChangePreview | null>(null)
  const toast = useToast()
  const [featurePlans, setFeaturePlans] = useState<Partial<Record<Feature, string[]>> | null>(null)

  useEffect(() => {
    let live = true
    getFeaturePlans().then((f) => live && setFeaturePlans(f))
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

  const openSwitch = (t: Tier) => {
    setSwitchTo(t)
    setPreview(null)
    run(async () => setPreview(await previewPlanChange(t.noTrial[interval])))
  }

  const confirmSwitch = (t: Tier) =>
    run(async () => {
      await changePlan(t.noTrial[interval])
      setSwitchTo(null)
      toast.show({ message: `You're now on ${t.name} ${interval === 'month' ? 'monthly' : 'yearly'}.` })
      await onChanged?.()
    })

  const subscribed = SUBSCRIBED.includes(billing?.status ?? '')
  const actions = billing ? billing.enabled && !billing.comped : true
  const period = interval === 'month' ? '/ month' : '/ year'
  const trial = !billing?.trial_used
  const { includes, extras } = split(featurePlans)

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
      <Button variant={variant} disabled={pending} onClick={() => (subscribed ? openSwitch(t) : subscribe(priceId))}>
        {subscribed ? switchLabel(billing.plan, t, interval) : trial ? 'Start free trial' : 'Subscribe'}
      </Button>
    )
  }

  return (
    <div className="plan-wrap grid gap-6">
      <div className="flex">
        <Tabs items={INTERVALS} value={interval} onChange={onInterval} aria-label="Billing interval" />
      </div>
      {billing && !billing.enabled ? <p className="text-body-sm text-secondary m-0">Billing isn't set up yet.</p> : null}
      {priceError ? <p className="text-body-sm text-secondary m-0">Prices couldn't load. Refresh the page to try again.</p> : null}
      <ul className="plan-grid">
        <PlanCard name={FREE.name} tagline={FREE.description} price="Free" period="" checks={[...FREE.limits, ...extras(FREE.id)]}>
          {!billing ? (
            <Link to="/signup" className="button button-primary">
              Get started
            </Link>
          ) : !subscribed && billing.plan === FREE.id ? (
            <p className="plan-current">Current plan</p>
          ) : null}
        </PlanCard>
        {TIERS.map((t) => (
          <PlanCard
            key={t.id}
            name={t.name}
            tagline={t.description}
            price={prices?.[t.priceId[interval]]}
            period={period}
            note={trial ? '7-day free trial' : undefined}
            featured={t.featured}
            checks={[...t.limits, ...extras(t.id)]}
          >
            {cta(t)}
          </PlanCard>
        ))}
      </ul>
      {switchTo ? null : <FormError>{error}</FormError>}
      <PlanIncludes items={includes} />
      <Dialog
        open={switchTo !== null}
        onClose={() => {
          setSwitchTo(null)
          setError(null)
        }}
        title={switchTo && billing ? `${switchLabel(billing.plan, switchTo, interval)}${billing.plan === switchTo.id ? ' billing' : ` ${interval === 'month' ? 'monthly' : 'yearly'}`}?` : ''}
      >
        <div className="grid gap-4">
          <div className="grid gap-1 text-body-sm text-secondary" aria-live="polite">
            {preview ? switchSummary(preview, interval).map((line) => <p key={line} className="m-0">{line}</p>) : error ? null : <p className="m-0">Working out the price…</p>}
          </div>
          <FormError>{error}</FormError>
          <Button variant="secondary" className="w-full" disabled={pending || !preview} onClick={() => switchTo && confirmSwitch(switchTo)}>
            {pending && preview ? 'Switching…' : switchTo && billing ? switchLabel(billing.plan, switchTo, interval) : ''}
          </Button>
        </div>
      </Dialog>
    </div>
  )
}
