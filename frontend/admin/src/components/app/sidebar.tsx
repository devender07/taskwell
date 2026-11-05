import { Link, useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { Logo } from '@taskwell/shared/logo'
import { api } from '@/api'
import './app.css'

export function Sidebar({ email }: { email: string }) {
  const navigate = useNavigate()
  const [signingOut, setSigningOut] = useState(false)

  async function signOut() {
    setSigningOut(true)
    await api('/auth/signout', new FormData()).catch(() => {})
    await navigate({ to: '/signin' })
  }

  return (
    <nav className="sidebar" aria-label="Admin">
      <span className="sidebar-logo">
        <Logo size={24} />
      </span>
      <Link to="/" activeOptions={{ exact: true }}>Overview</Link>
      <Link to="/users">Users</Link>
      <Link to="/workspaces">Workspaces</Link>
      <Link to="/config">Config</Link>
      <Link to="/audit">Audit log</Link>
      <div className="sidebar-foot">
        <span className="text-caption text-secondary ellipsis" title={email}>{email}</span>
        <button type="button" onClick={signOut} disabled={signingOut}>
          {signingOut ? 'Signing out…' : 'Sign out'}
        </button>
      </div>
    </nav>
  )
}
