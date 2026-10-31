import { Link, useNavigate, useParams } from '@tanstack/react-router'
import { useActionState, useState } from 'react'
import { Avatar } from '@tether/shared/avatar'
import { Button } from '@tether/shared/button'
import { Dialog } from '@tether/shared/dialog'
import { Logo } from '@tether/shared/logo'
import { Popover } from '@tether/shared/popover'
import { signOut as endSession, toFormErrors, type User, type Workspace } from '@/api'
import { CreateWorkspaceForm } from '@/components/workspace/create-form'
import { useUnread } from './unread'
import './navbar.css'

export function Navbar({ user, workspaces }: { user: User; workspaces: Workspace[] }) {
  return (
    <header className="navbar sticky top-0 z-sticky flex items-center justify-between px-4">
      <div className="flex items-center gap-3" style={{ minWidth: 0 }}>
        <Link to="/" aria-label="Tether home" className="navbar-home">
          <Logo size={24} />
        </Link>
        <WorkspaceSwitcher workspaces={workspaces} />
      </div>
      <div className="flex items-center gap-2">
        <InboxBell />
        <Popover
          aria-label="Account"
          placement="bottom-end"
          trigger={(props) => (
            <button type="button" className="navbar-avatar" aria-label="Account" {...props}>
              <Avatar name={user.name} src={user.avatar_url} />
            </button>
          )}
        >
          <AccountMenu user={user} />
        </Popover>
      </div>
    </header>
  )
}

function InboxBell() {
  const slug = useParams({ strict: false }).workspace
  const unread = useUnread()
  const count = unread.count + unread.chats
  if (!slug) return null
  return (
    <Link to="/$workspace/inbox/activity" params={{ workspace: slug }} className="navbar-bell" aria-label={count ? `Inbox, ${count} unread` : 'Inbox'}>
      <BellIcon />
      {count ? <span className="navbar-badge" aria-hidden>{count > 9 ? '9+' : count}</span> : null}
    </Link>
  )
}

// Only inside /$workspace routes; an unknown slug (not found) shows no switcher either.
function WorkspaceSwitcher({ workspaces }: { workspaces: Workspace[] }) {
  const slug = useParams({ strict: false }).workspace
  const current = workspaces.find((w) => w.slug === slug)
  const [creating, setCreating] = useState(false)
  if (!current) return null
  const closeDialog = () => setCreating(false)

  return (
    <>
      <Popover
        aria-label="Workspaces"
        placement="bottom-start"
        trigger={(props) => (
          <Button variant="ghost" aria-label={`Workspace: ${current.name}`} style={{ minWidth: 0, color: 'var(--text)', fontWeight: 510 }} {...props}>
            <span className="ellipsis" style={{ maxWidth: 200 }}>{current.name}</span>
            <ChevronIcon />
          </Button>
        )}
      >
        {(close) => (
          <nav style={{ width: 264 }}>
            <p className="popover-title">Workspaces</p>
            {workspaces.map((w) => (
              <Link
                key={w.id}
                to="/$workspace"
                params={{ workspace: w.slug }}
                preload={false} // the loader marks a workspace as opened; hovering must not reorder the list
                className="popover-item justify-between gap-2"
                aria-current={w.id === current.id ? 'page' : undefined}
                onClick={close}
              >
                <span className="ellipsis">{w.name}</span>
                {w.id === current.id ? <CheckIcon /> : null}
              </Link>
            ))}
            <hr className="popover-divider" />
            <Link to="/$workspace/settings/workspace" params={{ workspace: current.slug }} className="popover-item" onClick={close}>
              Workspace settings
            </Link>
            <button type="button" className="popover-item" onClick={() => { close(); setCreating(true) }}>
              Create workspace
            </button>
          </nav>
        )}
      </Popover>
      <Dialog open={creating} onClose={closeDialog} title="Create workspace" description="A workspace holds a team's projects and issues.">
        <CreateWorkspaceForm onDone={closeDialog} />
      </Dialog>
    </>
  )
}

function AccountMenu({ user }: { user: User }) {
  const navigate = useNavigate()
  const [error, signOut, pending] = useActionState(async () => {
    try {
      await endSession()
    } catch (e) {
      return toFormErrors(e).error
    }
    await navigate({ to: '/signin' })
    return null
  }, null)

  return (
    <div style={{ width: 264 }}>
      <div className="flex items-center gap-3 px-3 py-2">
        <Avatar name={user.name} size={36} src={user.avatar_url} />
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

function ChevronIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <path d="m6 9 6 6 6-6" />
    </svg>
  )
}

function CheckIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden className="flex-noshrink">
      <path d="M20 6 9 17l-5-5" />
    </svg>
  )
}
