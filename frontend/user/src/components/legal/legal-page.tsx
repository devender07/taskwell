import { Link } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { Logo } from '@taskwell/shared/logo'
import './legal.css'

const PAGES = [
  { to: '/privacy', label: 'Privacy notice' },
  { to: '/terms', label: 'Terms of service' },
  { to: '/subprocessors', label: 'Subprocessors' },
] as const

export const UPDATED = 'November 10, 2026'

export function LegalPage({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="legal">
      <header className="legal-header flex items-center gap-4">
        <a href="/" aria-label="Taskwell home" className="legal-home flex">
          <Logo size={24} />
        </a>
        <nav aria-label="Legal" className="flex flex-wrap gap-4">
          {PAGES.map((p) => (
            <Link key={p.to} to={p.to} activeProps={{ 'aria-current': 'page' }}>
              {p.label}
            </Link>
          ))}
        </nav>
      </header>
      <main className="legal-main grid gap-6">
        <header className="grid gap-2">
          <h1 className="text-h2 m-0">{title}</h1>
          <p className="text-caption text-secondary m-0">Last updated {UPDATED}</p>
          <p className="legal-draft text-body-sm m-0">Draft for legal review. Text in [brackets] is still to be filled in.</p>
        </header>
        {children}
      </main>
    </div>
  )
}
