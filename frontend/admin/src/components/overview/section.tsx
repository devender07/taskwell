import type { ReactNode } from 'react'

export function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="grid gap-4" aria-label={title}>
      <h2 className="text-label text-secondary">{title}</h2>
      {children}
    </section>
  )
}

export function Grid({ min = 160, children }: { min?: number; children: ReactNode }) {
  return (
    <div className="grid gap-4" style={{ gridTemplateColumns: `repeat(auto-fill, minmax(min(${min}px, 100%), 1fr))` }}>
      {children}
    </div>
  )
}

export function Tile({ label, value, sub, alert }: { label: string; value: number | string; sub?: ReactNode; alert?: boolean }) {
  return (
    <div className="grid gap-1 p-5 bg-surface border rounded-lg" style={{ alignContent: 'start' }}>
      <span className="text-caption text-secondary">{label}</span>
      <span className={alert ? 'text-h2 text-accent' : 'text-h2'}>{typeof value === 'number' ? value.toLocaleString(undefined) : value}</span>
      {sub ? <span className="text-caption text-secondary">{sub}</span> : null}
    </div>
  )
}
