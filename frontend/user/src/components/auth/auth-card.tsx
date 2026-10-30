import type { ReactNode } from 'react'
import { Button } from '@tether/shared/button'
import { LogoMark } from '@tether/shared/logo'
import './auth.css'

const line = { height: 1, background: 'rgba(255, 255, 255, 0.08)' }

export function AuthCard({ title, subtitle, footer, children }: { title: string; subtitle: string; footer?: ReactNode; children: ReactNode }) {
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
        {footer ? <p className="text-caption text-secondary text-center">{footer}</p> : null}
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

// Full-page navigation: the API redirects to Google and back. Google sign-ins are remembered.
export function GoogleButton() {
  return (
    <Button variant="secondary" className="w-full" onClick={() => location.assign('/api/auth/google?remember=1')}>
      <svg width="16" height="16" viewBox="0 0 48 48" aria-hidden>
        <path fill="#EA4335" d="M24 9.5c3.5 0 6.6 1.2 9 3.5l6.7-6.7C35.6 2.4 30.2 0 24 0 14.6 0 6.6 5.4 2.6 13.3l7.8 6C12.3 13.5 17.7 9.5 24 9.5z" />
        <path fill="#4285F4" d="M46.1 24.5c0-1.6-.1-3.1-.4-4.5H24v9h12.4c-.5 2.9-2.2 5.3-4.6 6.9l7.5 5.8c4.4-4 6.8-10 6.8-17.2z" />
        <path fill="#FBBC05" d="M10.4 28.7A14.5 14.5 0 0 1 9.5 24c0-1.6.3-3.2.9-4.7l-7.8-6A24 24 0 0 0 0 24c0 3.9.9 7.5 2.6 10.7l7.8-6z" />
        <path fill="#34A853" d="M24 48c6.5 0 11.9-2.1 15.9-5.8l-7.5-5.8c-2.1 1.4-4.8 2.3-8.4 2.3-6.3 0-11.7-4.2-13.6-9.9l-7.8 6C6.6 42.6 14.6 48 24 48z" />
      </svg>
      Continue with Google
    </Button>
  )
}
