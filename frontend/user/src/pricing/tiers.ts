// The paid plans on the pricing table. Price ids are the Paddle catalog's (docs/billing-plan.md); the backend maps a
// subscription to a plan by its price's custom_data.taskwell_plan, so a new price needs that set in Paddle too.
export interface Tier {
  id: 'starter' | 'pro' | 'advanced'
  name: 'Starter' | 'Pro' | 'Advanced'
  description: string
  features: string[]
  priceId: { month: string; year: string }
  featured?: boolean
}

export type Interval = keyof Tier['priceId']

export const TIERS: Tier[] = [
  {
    id: 'starter',
    name: 'Starter',
    description: 'For small teams getting started',
    features: ['1 workspace', '5 projects per workspace', 'Up to 10 members per workspace', '20 GB storage per workspace', 'Every Taskwell feature'],
    priceId: {
      month: 'pri_01m6wk3cmqzf6zhwrsdhq79xhx',
      year: 'pri_01m6wk3cxd0b6mdhphsj67m1hk',
    },
  },
  {
    id: 'pro',
    name: 'Pro',
    description: 'For growing teams',
    features: ['3 workspaces', 'Unlimited projects', 'Up to 25 members per workspace', '100 GB storage per workspace', 'Every Taskwell feature'],
    priceId: {
      month: 'pri_01m6wk3df6zkt76e4zsz6q1x8a',
      year: 'pri_01m6wk3dqt1m882m8a68khw5fk',
    },
    featured: true,
  },
  {
    id: 'advanced',
    name: 'Advanced',
    description: 'For larger teams and lots of files',
    features: ['Unlimited workspaces', 'Unlimited projects', 'Unlimited members', 'Unlimited storage', 'Every Taskwell feature'],
    priceId: {
      month: 'pri_01m6wk3eavpqzpc1gjs4ace2p9',
      year: 'pri_01m6wk3ekevk50xcfe07vjxbnk',
    },
  },
]

export const PRICE_IDS = TIERS.flatMap((t) => [t.priceId.month, t.priceId.year])

// The plan name for a plan id from the API, Free included.
export const planName = (id: string) => (id === 'free' ? 'Free' : (TIERS.find((t) => t.id === id)?.name ?? id))
