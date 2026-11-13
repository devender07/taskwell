import { useTransition, useState } from 'react'
import { createFileRoute, notFound, useRouter } from '@tanstack/react-router'
import { Button } from '@taskwell/shared/button'
import { longDate, shortDate } from '@taskwell/shared/time'
import { api, billingPath, openBillingPortal, storagePath, toFormErrors, workspaceBillingPath, type Billing, type WorkspaceBilling } from '@/api'
import { trackPath } from '@/realtime'
import { FormError } from '@/components/auth/auth-card'
import { formatBytes } from '@/components/files/bytes'
import { AddPaymentMethod } from '@/components/app/trial-banner'
import { PricingTable, SUBSCRIBED, validateInterval } from '@/pricing/pricing-table'
import { planName, type Interval } from '@/pricing/tiers'
import '@/components/home/home.css'

export const Route = createFileRoute('/_app/$workspace/_main/settings/billing')({
  validateSearch: (search): { interval?: Interval } => ({
    interval: validateInterval(search.interval),
  }),
  beforeLoad: ({ context }) => {
    if (context.beta) throw notFound()
  },
  loader: async (ctx) => {
    const path = workspaceBillingPath(ctx.params.workspace)
    trackPath(ctx, path)
    trackPath(ctx, storagePath(ctx.params.workspace))
    trackPath(ctx, billingPath)
    const [workspace, account] = await Promise.all([api<WorkspaceBilling>(path), api<Billing>(billingPath)])
    return { workspace, account }
  },
  component: BillingSettings,
})

function statusLine(b: Billing) {
  if (b.comped) return b.comped_until ? `Comped by Taskwell until ${longDate(b.comped_until)}` : 'Comped by Taskwell'
  if (b.status === 'past_due') return 'Payment failed. Update your payment method.'
  const parts = [b.interval === 'year' ? 'Yearly' : b.interval === 'month' ? 'Monthly' : '', b.status === 'trialing' ? 'Free trial' : '']
  if (b.needs_payment_method && b.trial_ends_at) parts.push(`Ends on ${shortDate(b.trial_ends_at)} unless you add a payment method`)
  else if (b.renews_at) parts.push(b.cancel_at_period_end ? `Cancels on ${shortDate(b.renews_at)}` : `Renews on ${shortDate(b.renews_at)}`)
  return parts.filter(Boolean).join(' · ')
}

function ownerLine(w: WorkspaceBilling, name: string) {
  if (w.is_owner) return `On your ${name} plan`
  return w.owner ? `On ${name}, owned by ${w.owner.name}` : `On ${name}`
}

function BillingSettings() {
  const { workspace, account } = Route.useLoaderData()
  const interval = Route.useSearch().interval ?? account.interval ?? 'month'
  const navigate = Route.useNavigate()
  const router = useRouter()
  const [error, setError] = useState<string | null>(null)
  const [pending, startTransition] = useTransition()
  const line = statusLine(account)

  const manage = () =>
    startTransition(async () => {
      setError(null)
      try {
        location.assign((await openBillingPortal()).url)
      } catch (e) {
        setError(toFormErrors(e).error)
      }
    })

  return (
    <main className="grid gap-6 w-full px-8 py-10" style={{ alignContent: 'start' }}>
      <header className="page-top grid gap-2">
        <h1 className="text-h2">Billing</h1>
        <p className="text-body-sm text-secondary">One price for your account, covering every workspace you create.</p>
      </header>
      <section className="grid gap-4 p-6 bg-surface border rounded-lg" aria-labelledby="workspace-plan-title">
        <div className="grid gap-1">
          <h2 id="workspace-plan-title" className="text-label">
            This workspace
          </h2>
          <p className="text-body-sm text-secondary m-0">{ownerLine(workspace, planName(workspace.plan))}</p>
        </div>
        <Meter label="Projects" used={workspace.projects.used} limit={workspace.projects.limit} format={String} />
        <Meter label="Members" used={workspace.members.used} limit={workspace.members.limit} format={String} />
        <Meter label="Storage" used={workspace.storage.used_bytes} limit={workspace.storage.limit_bytes} format={formatBytes} />
      </section>
      <section className="grid gap-4 p-6 bg-surface border rounded-lg" aria-labelledby="account-plan-title">
        <div className="grid gap-1">
          <h2 id="account-plan-title" className="text-label">
            Your plan
          </h2>
          <p className="text-body-sm m-0">
            {planName(account.plan)}
            {line ? <span className={account.status === 'past_due' ? 'text-accent' : 'text-secondary'}> · {line}</span> : null}
          </p>
        </div>
        <Meter label="Workspaces you own" used={account.workspaces.used} limit={account.workspaces.limit} format={String} />
        {account.enabled && (SUBSCRIBED.includes(account.status ?? '') || account.trial_used) ? (
          <div className="flex flex-wrap gap-3">
            {account.needs_payment_method ? <AddPaymentMethod onError={setError} /> : null}
            <Button variant={account.needs_payment_method ? 'secondary' : 'primary'} disabled={pending} onClick={manage}>
              Manage billing
            </Button>
          </div>
        ) : null}
        <FormError>{error}</FormError>
      </section>
      <PricingTable
        interval={interval}
        onInterval={(i) => navigate({ search: { interval: i }, replace: true })}
        billing={account}
        onChanged={() => router.invalidate()}
      />
    </main>
  )
}

function Meter({ label, used, limit, format }: { label: string; used: number; limit: number | null; format: (n: number) => string }) {
  const text = limit === null ? `${format(used)} · Unlimited` : `${format(used)} of ${format(limit)}`
  return (
    <div className="grid gap-2" style={{ maxWidth: 400 }}>
      <span className="flex justify-between text-body-sm">
        <span>{label}</span>
        <span className="text-secondary">{text}</span>
      </span>
      {limit === null ? null : (
        <span
          role="progressbar"
          aria-label={`${label} used`}
          aria-valuemin={0}
          aria-valuemax={limit}
          aria-valuenow={used}
          aria-valuetext={text}
          className="home-bar"
        >
          <span
            style={{
              width: limit ? `${Math.min(100, (used / limit) * 100)}%` : 0,
            }}
          />
        </span>
      )}
    </div>
  )
}
