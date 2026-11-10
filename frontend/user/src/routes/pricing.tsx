import { Link, createFileRoute, useRouter } from '@tanstack/react-router'
import { Logo } from '@taskwell/shared/logo'
import { api, billingPath, getMe, type Billing } from '@/api'
import { PricingTable, validateInterval } from '@/pricing/pricing-table'
import type { Interval } from '@/pricing/tiers'
import '@/pricing/pricing.css'

// _ptxn: a Paddle payment link's transaction; Paddle.js opens its checkout when it loads here.
type Search = { interval?: Interval; _ptxn?: string }

// Outside _app: visitors see it signed out; a signed-in account (or an unreachable API) never forces sign-in.
export const Route = createFileRoute('/pricing')({
  validateSearch: (search): Search => ({
    interval: validateInterval(search.interval),
    ...(typeof search._ptxn === 'string' ? { _ptxn: search._ptxn } : {}),
  }),
  loader: async () => {
    const user = await getMe().catch(() => null)
    return { billing: user ? await api<Billing>(billingPath) : null }
  },
  component: Pricing,
})

const FAQ = [
  [
    'Who pays?',
    "The person who creates workspaces. One plan covers every workspace you create and everyone in them, up to its limits. Members you invite don't need a plan.",
  ],
  ['How does the free trial work?', 'Your first subscription starts with 7 days free, once per account. Add a card at checkout; you are charged when the trial ends unless you cancel first.'],
  ['What do we get without a plan?', 'Free gives you 1 workspace with 1 project and 2 GB of storage, for you alone. To invite your team, choose a paid plan.'],
  [
    'What happens if we go over a limit?',
    "Nothing is deleted. You can't create more workspaces or projects, invite more members or upload more files until you upgrade or make room.",
  ],
  ['Can we cancel any time?', 'Yes. Cancel from billing settings and keep your plan until the end of the period you paid for, then you move to Free.'],
  ['How can we pay?', 'Cards, PayPal and more, depending on your country.'],
  ['Do we get tax invoices?', 'Yes. Paddle, our reseller, emails a tax invoice for every payment. You can also find them under Manage billing.'],
]

function Pricing() {
  const { interval = 'month' } = Route.useSearch()
  const { billing } = Route.useLoaderData()
  const navigate = Route.useNavigate()
  const router = useRouter()

  return (
    <div className="pricing-page grid gap-12">
      <header className="pricing-top">
        <a href="/" aria-label="Taskwell home">
          <Logo />
        </a>
        <Link to="/signin" className="button button-primary">
          {billing ? 'Go to dashboard' : 'Sign in'}
        </Link>
      </header>
      <main className="grid gap-12">
        <header className="pricing-head grid gap-4">
          <h1>Plans for every team</h1>
          <p className="text-secondary m-0" style={{ maxWidth: 640 }}>
            One price for your account covers every workspace you create. Your first subscription starts with a 7-day free trial.
          </p>
        </header>
        <PricingTable
          interval={interval}
          onInterval={(i) =>
            navigate({
              search: (prev) => ({ ...prev, interval: i }),
              replace: true,
            })
          }
          billing={billing}
          onChanged={() => router.invalidate()}
        />
        <section className="faq" aria-labelledby="faq">
          <h2 id="faq" className="text-h2">
            Questions
          </h2>
          {FAQ.map(([q, a]) => (
            <details key={q}>
              <summary>{q}</summary>
              <p>{a}</p>
            </details>
          ))}
        </section>
      </main>
    </div>
  )
}
