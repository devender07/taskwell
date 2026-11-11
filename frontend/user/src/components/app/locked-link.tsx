import { Link } from '@tanstack/react-router'
import type { CSSProperties, ComponentType, ReactNode } from 'react'
import type { Feature, PlanId } from '@/api'
import { planName } from '@/pricing/tiers'

export function LockedLink({ workspace, feature, plan, Icon, onClick, children }: { workspace: string; feature: Feature; plan: PlanId; Icon: ComponentType<{ style?: CSSProperties }>; onClick?: () => void; children: ReactNode }) {
  return (
    <Link to="/$workspace/upgrade" params={{ workspace }} search={{ feature }} onClick={onClick}>
      <Icon style={{ opacity: 0.5 }} />
      {children}
      <span className="text-caption flex-noshrink px-2 rounded-full" style={{ marginLeft: 'auto', border: '1px solid rgba(var(--ink), 0.08)' }}>
        <span className="sr-only">Available on </span>
        {planName(plan)}
      </span>
    </Link>
  )
}
