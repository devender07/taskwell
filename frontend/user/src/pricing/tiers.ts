// The paid plans on the pricing table. Price ids are the Paddle catalog's (docs/billing-plan.md); the backend maps a
// subscription to a plan by its price's custom_data.taskwell_plan, so a new price needs that set in Paddle too.
export interface Tier {
  id: 'starter' | 'pro' | 'advanced'
  name: 'Starter' | 'Pro' | 'Advanced'
  description: string
  limits: string[]
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
    limits: ['1 workspace', '5 projects per workspace', 'Up to 10 members per workspace', '20 GB storage per workspace'],
    priceId: {
      month: 'pri_01m6wk3cmqzf6zhwrsdhq79xhx',
      year: 'pri_01m6wk3cxd0b6mdhphsj67m1hk',
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
    limits: ['3 workspaces', 'Unlimited projects', 'Up to 25 members per workspace', '100 GB storage per workspace'],
    priceId: {
      month: 'pri_01m6wk3df6zkt76e4zsz6q1x8a',
      year: 'pri_01m6wk3dqt1m882m8a68khw5fk',
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
    limits: ['Unlimited workspaces', 'Unlimited projects', 'Unlimited members', 'Unlimited storage'],
    priceId: {
      month: 'pri_01m6wk3eavpqzpc1gjs4ace2p9',
      year: 'pri_01m6wk3ekevk50xcfe07vjxbnk',
    },
    noTrial: {
      month: 'pri_01m6wq0tzz40vgs3g3sw7c4sct',
      year: 'pri_01m6wq0vg2br8ejp563cw79zqc',
    },
  },
]

// The free plan: no price, no checkout. Its limits match backend/user/internal/plan.
export const FREE = {
  id: 'free',
  name: 'Free',
  description: 'For trying Taskwell',
  limits: ['1 workspace', '1 project', 'Just you, no other members', '2 GB storage'],
} as const

export const PRICE_IDS = TIERS.flatMap((t) => [t.priceId.month, t.priceId.year])

// The plan name for a plan id from the API, Free included.
export const planName = (id: string) => (id === 'free' ? 'Free' : (TIERS.find((t) => t.id === id)?.name ?? id))
