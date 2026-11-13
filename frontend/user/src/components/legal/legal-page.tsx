import { Link } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { Logo } from '@taskwell/shared/logo'
import { useBeta } from '@/api'
import './legal.css'

const PAGES = [
  { to: '/privacy', label: 'Privacy notice' },
  { to: '/terms', label: 'Terms of service' },
  { to: '/subprocessors', label: 'Subprocessors' },
] as const

const BETA_PAGES = [
  { to: '/terms', label: 'Beta notice' },
  { to: '/subprocessors', label: 'Subprocessors' },
] as const

export const UPDATED = 'November 10, 2026'

export function LegalPage({ title, children }: { title: string; children: ReactNode }) {
  const beta = useBeta()
  return (
    <div className="legal">
      <header className="legal-header flex items-center gap-4">
        <a href="/" aria-label="Taskwell home" className="legal-home flex">
          <Logo size={24} />
        </a>
        <nav aria-label="Legal" className="flex flex-wrap gap-4">
          {(beta ? BETA_PAGES : PAGES).map((p) => (
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
          {beta ? null : <p className="legal-draft text-body-sm m-0">Draft for legal review. Text in [brackets] is still to be filled in.</p>}
        </header>
        {children}
      </main>
    </div>
  )
}

export function BetaNotice() {
  return (
    <LegalPage title="Beta notice">
      <p>
        <strong className="font-590">Taskwell is in beta.</strong> It's free while it's in beta, run by an independent developer, and offered as is: it
        may change, have bugs or be unavailable, and it comes with no warranty. Don't rely on it for anything critical, and keep your own copies of
        anything important. We'll email you before the beta ends or before anything you made is deleted.
      </p>
      <p>
        <strong className="font-590">What we collect.</strong> Your name, email and password (stored hashed), what you and your team put in Taskwell
        (workspaces, projects, issues, comments, notes, files), and technical data needed to run it: sign-in sessions, IP addresses and browser
        details for security, and error logs.
      </p>
      <p>
        <strong className="font-590">What we do with it.</strong> Only run Taskwell: sign you in, show your work to the people you share it with, send
        the emails you asked for, and keep the service secure. We don't sell it or use it for ads. The services that host it and send email for us
        are listed under <Link to="/subprocessors">Subprocessors</Link>.
      </p>
      <p>
        <strong className="font-590">Your choices.</strong> You can export your data and delete your account under Settings → Your data. For anything
        else, email <a href="mailto:dev.taskwell@gmail.com">dev.taskwell@gmail.com</a>.
      </p>
      <p>
        <strong className="font-590">Using Taskwell.</strong> You must be 18 or older. Don't use it for anything illegal, to harm others, or to try to
        break it. We can suspend accounts that do.
      </p>
    </LegalPage>
  )
}
