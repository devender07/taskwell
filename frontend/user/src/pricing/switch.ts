import { money } from '@taskwell/shared/money'
import { longDate } from '@taskwell/shared/time'
import type { ChangePreview } from '@/api'
import { TIERS, type Interval, type Tier } from './tiers.ts'

// What moving from the current plan and interval to tier at interval is called, on the button and the dialog.
export function switchLabel(plan: string, tier: Tier, interval: Interval) {
  const rank = (id: string) => TIERS.findIndex((t) => t.id === id)
  if (plan === tier.id) return interval === 'year' ? 'Switch to yearly' : 'Switch to monthly'
  return rank(tier.id) > rank(plan) ? `Upgrade to ${tier.name}` : `Downgrade to ${tier.name}`
}

// What a switch costs, from the API's preview: what happens today, then the price of each period after.
export function switchSummary(p: ChangePreview, interval: Interval): [string, string] {
  const each = `${money(Number(p.recurring), p.currency)} / ${interval}`
  const from = p.next_billed_at ? ` from ${longDate(p.next_billed_at)}` : ''
  if (p.trialing) return [p.next_billed_at ? `Your free trial continues until ${longDate(p.next_billed_at)}.` : 'Your free trial continues.', `Then ${each}.`]
  const amount = p.today ? money(Number(p.today.amount), p.currency) : ''
  const today = !p.today
    ? 'Nothing to pay today.'
    : p.today.action === 'credit'
      ? `You'll get ${amount} in credit for the time left on your current plan, used for your next payments.`
      : `You'll pay ${amount} today, after credit for the time left on your current plan.`
  return [today, `Then ${each}${from}.`]
}
