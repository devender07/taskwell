import { Link, useNavigate } from '@tanstack/react-router'
import { useActionState } from 'react'
import { Avatar } from '@tether/shared/avatar'
import { IconButton } from '@tether/shared/icon-button'
import { Logo } from '@tether/shared/logo'
import { Popover } from '@tether/shared/popover'
import { api, toFormErrors, type User } from '@/api'
import './navbar.css'

export function Navbar({ user }: { user: User }) {
  return (
    <header className="navbar sticky top-0 z-sticky flex items-center justify-between px-4">
      <Link to="/" aria-label="Tether home" className="navbar-home">
        <Logo size={24} />
      </Link>
      <div className="flex items-center gap-2">
        <Popover
          aria-label="Notifications"
          placement="bottom-end"
          trigger={(props) => (
            <IconButton aria-label="Notifications" {...props}>
              <BellIcon />
            </IconButton>
          )}
        >
          <p className="popover-title">Notifications</p>
          <p className="px-3 py-6 text-caption text-secondary text-center" style={{ width: 264 }}>
            You're all caught up.
          </p>
        </Popover>
        <Popover
          aria-label="Account"
          placement="bottom-end"
          trigger={(props) => (
            <button type="button" className="navbar-avatar" aria-label="Account" {...props}>
              <Avatar name={user.name} />
            </button>
          )}
        >
          <AccountMenu user={user} />
        </Popover>
      </div>
    </header>
  )
}

function AccountMenu({ user }: { user: User }) {
  const navigate = useNavigate()
  const [error, signOut, pending] = useActionState(async () => {
    try {
      await api('/auth/signout', new FormData())
    } catch (e) {
      return toFormErrors(e).error
    }
    await navigate({ to: '/signin' })
    return null
  }, null)

  return (
    <div style={{ width: 264 }}>
      <div className="flex items-center gap-3 px-3 py-2">
        <Avatar name={user.name} size={36} />
        <div className="grid" style={{ minWidth: 0 }}>
          <span className="text-label text-primary ellipsis">{user.name}</span>
          <span className="text-caption text-secondary ellipsis">{user.email}</span>
        </div>
      </div>
      <hr className="popover-divider" />
      <form action={signOut}>
        <button type="submit" className="popover-item" disabled={pending}>
          {pending ? 'Signing out…' : 'Sign out'}
        </button>
      </form>
      {error ? <p role="alert" className="px-3 py-2 text-caption text-accent">{error}</p> : null}
    </div>
  )
}

function BellIcon() {
  return (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" />
      <path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
    </svg>
  )
}
