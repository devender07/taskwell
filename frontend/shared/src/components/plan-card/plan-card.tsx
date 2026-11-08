import type { ReactNode } from 'react'
import { CheckIcon } from '../icons/icons'
import './plan-card.css'

export const PLAN_TAGLINES: Record<'starter' | 'pro' | 'ultimate', string> = {
  starter: 'For small teams trying Taskwell',
  pro: 'For growing teams',
  ultimate: 'For larger teams and lots of files',
}

const INCLUDES = ['Projects and issues', 'List and board views', 'Cycles and roadmap', 'Notes', 'Chat', 'AI testing', 'MCP and API', 'Webhooks', 'GitHub and GitLab']

type PlanCardProps = {
  name: string
  tagline: string
  price?: string // "Free" or a formatted amount; undefined while prices are unknown
  featured?: boolean
  checks: string[]
  children?: ReactNode // the CTA
}

export function PlanCard({ name, tagline, price, featured, checks, children }: PlanCardProps) {
  return (
    <li className="plan-card" data-featured={featured || undefined}>
      <div className="plan-head">
        <h3 className="plan-name">{name}</h3>
        {featured ? <span className="plan-badge">Most popular</span> : null}
      </div>
      <p className="plan-tagline">{tagline}</p>
      <p className="plan-price">
        {price === undefined ? null : <span className="plan-amount">{price}</span>}
        {price === undefined || price === 'Free' ? null : <span className="plan-period">/ month</span>}
      </p>
      {children ?? <span aria-hidden="true" />}
      <ul className="plan-checks">
        {checks.map((c) => (
          <li key={c}>
            <CheckIcon size={16} />
            {c}
          </li>
        ))}
      </ul>
    </li>
  )
}

export function PlanIncludes() {
  return (
    <section className="plan-includes" aria-labelledby="plan-includes">
      <h3 id="plan-includes">Every plan includes</h3>
      <ul>
        {INCLUDES.map((i) => (
          <li key={i}>
            <CheckIcon size={16} />
            {i}
          </li>
        ))}
      </ul>
      <p>Prices exclude tax, added at checkout. Customers in India pay in INR, by card or UPI.</p>
    </section>
  )
}
