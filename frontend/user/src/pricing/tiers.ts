// The paid plans on the pricing table. Price ids are the Paddle catalog's (docs/billing-plan.md); the backend maps a
// subscription to a plan by its price's custom_data.taskwell_plan, so a new price needs that set in Paddle too.
import type { PlanLimits } from '@/api'

export interface Tier {
  id: 'starter' | 'pro' | 'advanced'
  name: 'Starter' | 'Pro' | 'Advanced'
  description: string
  // With the 14-day trial that needs no payment method.
  priceId: { month: string; year: string }
  // The same prices without the trial, for an account that has had a subscription (one trial per account).
  noTrial: { month: string; year: string }
  featured?: boolean
}

export type Interval = keyof Tier['priceId']

export const TIERS: Tier[] = [
  {
    id: 'starter',
    name: 'Starter',
    description: 'For small teams getting started',
    priceId: {
      month: 'pri_01m72yye2dvs5yqvx6x22p88cy',
      year: 'pri_01m72yyegrsqg7tnre3zhw4qac',
    },
    noTrial: {
      month: 'pri_01m6wq0rxmnyjvzjex4bf82vcj',
      year: 'pri_01m6wq0sdxk937m927rr54d6g3',
    },
  },
  {
    id: 'pro',
    name: 'Pro',
    description: 'For growing teams',
    priceId: {
      month: 'pri_01m72yyfg8g5dvvhqp5nak1jrw',
      year: 'pri_01m72yygh7tmxfy26hyaddky1k',
    },
    noTrial: {
      month: 'pri_01m6wq0sxs3t3h70px2h59y38b',
      year: 'pri_01m6wq0tfs1z76jdtnsp5av839',
    },
    featured: true,
  },
  {
    id: 'advanced',
    name: 'Advanced',
    description: 'For larger teams and lots of files',
    priceId: {
      month: 'pri_01m72yyh5crybbt6kmd2xny3p8',
      year: 'pri_01m72yyhk6r1wds1tb2tk1tbwm',
    },
    noTrial: {
      month: 'pri_01m6wq0tzz40vgs3g3sw7c4sct',
      year: 'pri_01m6wq0vg2br8ejp563cw79zqc',
    },
  },
]

// The free plan: no price, no checkout.
export const FREE = {
  id: 'free',
  name: 'Free',
  description: 'For trying Taskwell',
} as const

export const TRIAL = '14-day free trial, no card needed'

export const PRICE_IDS = TIERS.flatMap((t) => [t.priceId.month, t.priceId.year])

// The plan name for a plan id from the API, Free included.
export const planName = (id: string) => (id === 'free' ? 'Free' : (TIERS.find((t) => t.id === id)?.name ?? id))

// A plan's limits as the lines on its card, from the API's numbers (0 is unlimited; for AI test runs, none).
// Projects, members and storage are per workspace, said only when the plan has more than one workspace.
export function limitLines(p: PlanLimits): string[] {
  const each = p.workspaces === 1 ? '' : ' per workspace'
  const count = (n: number, noun: string) => (n === 0 ? `Unlimited ${noun}s` : `${n} ${noun}${n === 1 ? '' : 's'}`)
  return [
    count(p.workspaces, 'workspace'),
    count(p.projects, 'project') + (p.projects === 0 ? '' : each),
    p.members === 1 ? 'Just you, no other members' : p.members === 0 ? 'Unlimited members' : `Up to ${p.members} members${each}`,
    p.storage_gb === 0 ? 'Unlimited storage' : `${p.storage_gb} GB storage${each}`,
    ...(p.ai_usd > 0 ? [`$${p.ai_usd} of AI test runs a month`] : []),
  ]
}
