import { PLANS, type Stats } from '@/api'

export function PlanBars({ plans }: { plans: Stats['plans'] }) {
  const entries = Object.entries(PLANS) as [keyof typeof PLANS, string][]
  const total = entries.reduce((n, [k]) => n + plans[k], 0)
  return (
    <div className="grid gap-4 p-5 bg-surface border rounded-lg">
      <h3 className="text-label">Accounts by plan</h3>
      <ul className="grid gap-3 m-0 p-0" style={{ listStyle: 'none' }}>
        {entries.map(([key, label]) => {
          const share = total ? Math.round((plans[key] / total) * 100) : 0
          return (
            <li key={key} className="grid gap-1">
              <div className="flex justify-between gap-3 text-body-sm">
                <span>{label}</span>
                <span className="text-secondary" style={{ fontVariantNumeric: 'tabular-nums' }}>
                  {plans[key].toLocaleString(undefined)} · {share}%
                </span>
              </div>
              <div className="plan-track" role="img" aria-label={`${label}: ${plans[key]} accounts, ${share}%`}>
                <div style={{ width: `${share}%` }} />
              </div>
            </li>
          )
        })}
      </ul>
    </div>
  )
}
