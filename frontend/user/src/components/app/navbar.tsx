import { Link, useMatch, useNavigate, useParams } from '@tanstack/react-router'
import { createContext, use, useActionState, useState, type ReactNode } from 'react'
import { Avatar } from '@taskwell/shared/avatar'
import { Button } from '@taskwell/shared/button'
import { Dialog } from '@taskwell/shared/dialog'
import { Logo } from '@taskwell/shared/logo'
import { Popover } from '@taskwell/shared/popover'
import { CheckIcon, ChevronDownIcon, MenuIcon, XIcon } from '@taskwell/shared/icons'
import { signOut as endSession, toFormErrors, type User, type Workspace } from '@/api'
import { CreateWorkspaceForm } from '@/components/workspace/create-form'
import { NotificationsBell } from './notifications-bell'
import './navbar.css'

// Whether the app sidebar is open as the mobile menu; the navbar's button toggles it.
const MenuContext = createContext<[boolean, (open: boolean) => void]>([false, () => {}])
export const useMenu = () => use(MenuContext)
export function MenuProvider({ children }: { children: ReactNode }) {
  return <MenuContext value={useState(false)}>{children}</MenuContext>
}

export function Navbar({ user, workspaces }: { user: User; workspaces: Workspace[] }) {
  const home = useParams({ strict: false }).workspace ?? workspaces[0]?.slug
  const hasSidebar = useMatch({ from: '/_app/$workspace/_main', shouldThrow: false })
  const [menuOpen, setMenuOpen] = useMenu()
  return (
    <header className="navbar sticky top-0 z-sticky flex items-center justify-between px-4">
      <div className="flex items-center gap-3" style={{ minWidth: 0 }}>
        {hasSidebar ? (
          <button
            type="button"
            className="navbar-menu"
            aria-controls="app-sidebar"
            aria-expanded={menuOpen}
            aria-label={menuOpen ? 'Close menu' : 'Open menu'}
            onClick={() => setMenuOpen(!menuOpen)}
          >
            {menuOpen ? <XIcon size={20} /> : <MenuIcon size={20} />}
          </button>
        ) : null}
        <Link to={home ? '/$workspace' : '/new-workspace'} params={{ workspace: home }} aria-label="Taskwell home" className="navbar-home">
          <Logo size={24} />
        </Link>
      </div>
      <div className="flex items-center gap-2">
        <NotificationsBell />
        <Popover
          aria-label="Account"
          placement="bottom-end"
          trigger={(props) => (
            <button type="button" className="navbar-avatar" aria-label="Account" {...props}>
              <Avatar name={user.name} src={user.avatar_url} />
            </button>
          )}
        >
          {(close) => <AccountMenu user={user} onNavigate={close} />}
        </Popover>
      </div>
    </header>
  )
}

// Only inside /$workspace routes; an unknown slug (not found) shows no switcher either.
export function WorkspaceSwitcher({ workspaces, canCreate }: { workspaces: Workspace[]; canCreate: boolean }) {
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
            <ChevronDownIcon size={14} strokeWidth={2} />
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
                {w.guest ? <span className="text-caption text-secondary flex-noshrink" style={{ marginLeft: 'auto' }}>Guest</span> : null}
                {w.id === current.id ? <CheckIcon size={16} strokeWidth={2} className="flex-noshrink" /> : null}
              </Link>
            ))}
            <hr className="popover-divider" />
            {current.guest ? null : (
              <Link to="/$workspace/settings/workspace" params={{ workspace: current.slug }} className="popover-item" onClick={close}>
                Workspace settings
              </Link>
            )}
            {canCreate ? (
              <button type="button" className="popover-item" onClick={() => { close(); setCreating(true) }}>
                Create workspace
              </button>
            ) : null}
          </nav>
        )}
      </Popover>
      <Dialog open={creating} onClose={closeDialog} title="Create workspace" description="A workspace holds a team's projects and issues.">
        <CreateWorkspaceForm onDone={closeDialog} />
      </Dialog>
    </>
  )
}

function AccountMenu({ user, onNavigate }: { user: User; onNavigate: () => void }) {
  const navigate = useNavigate()
  const slug = useParams({ strict: false }).workspace
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
      {slug ? (
        <Link to="/$workspace/members/$id" params={{ workspace: slug, id: user.id }} className="popover-item" onClick={onNavigate}>
          View your analytics
        </Link>
      ) : null}
      <form action={signOut}>
        <button type="submit" className="popover-item" disabled={pending}>
          {pending ? 'Signing out…' : 'Sign out'}
        </button>
      </form>
      {error ? <p role="alert" className="px-3 py-2 text-caption text-accent">{error}</p> : null}
    </div>
  )
}

