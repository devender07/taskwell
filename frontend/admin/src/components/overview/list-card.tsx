import { Link } from '@tanstack/react-router'
import type { ReactNode } from 'react'

export type Row = { key: string; to: '/users' | '/workspaces'; q: string; title: string; sub: ReactNode; end: ReactNode }

export function ListCard({ title, rows, empty, action }: { title: string; rows: Row[]; empty: string; action?: ReactNode }) {
  return (
    <section className="grid bg-surface border rounded-lg overflow-hidden" style={{ alignContent: 'start' }}>
      <div className="flex items-center justify-between gap-3 px-5 py-4">
        <h3 className="text-label">{title}</h3>
        {action}
      </div>
      {rows.length === 0 ? (
        <p className="text-body-sm text-secondary px-5 pb-5 m-0">{empty}</p>
      ) : (
        rows.map((r) => (
          <Link key={r.key} to={r.to} search={{ q: r.q }} className="overview-row">
            <span className="grid flex-grow" style={{ minWidth: 0 }}>
              <span className="text-body-sm font-510 ellipsis">{r.title}</span>
              <span className="text-caption text-secondary ellipsis">{r.sub}</span>
            </span>
            <span className="text-caption text-secondary flex-noshrink flex items-center gap-2">{r.end}</span>
          </Link>
        ))
      )}
    </section>
  )
}
