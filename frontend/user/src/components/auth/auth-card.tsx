import type { ReactNode } from 'react'
import { LogoMark } from '@tether/shared/logo'
import './auth.css'

const line = { height: 1, background: 'rgba(255, 255, 255, 0.08)' }

export function AuthCard({ title, subtitle, footer, children }: { title: string; subtitle: string; footer: ReactNode; children: ReactNode }) {
  return (
    <main className="grid place-center min-h-screen p-4">
      <div className="grid gap-5 w-full p-8 bg-surface border rounded-lg" style={{ maxWidth: 400 }}>
        <header className="grid justify-items-center gap-2 text-center">
          <span className="mb-2">
            <LogoMark />
          </span>
          <h1 className="text-h2">{title}</h1>
          <p className="text-body-sm text-secondary">{subtitle}</p>
        </header>
        {children}
        <p className="text-caption text-secondary text-center">{footer}</p>
      </div>
    </main>
  )
}

export function OrDivider() {
  return (
    <div className="grid items-center gap-3 text-caption text-secondary" style={{ gridTemplateColumns: '1fr auto 1fr' }}>
      <span style={line} />
      or
      <span style={line} />
    </div>
  )
}

export function FormError({ children }: { children?: string | null }) {
  return children ? <p role="alert" className="text-caption text-accent">{children}</p> : null
}
