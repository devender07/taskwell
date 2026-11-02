import { Link, useLocation } from '@tanstack/react-router'
import { FolderIcon, HomeIcon, InboxIcon, SearchIcon, SettingsIcon, UserIcon } from '@tether/shared/icons'
import { useState } from 'react'
import { isApple, paletteKey, useShortcuts } from './shortcuts'
import { useUnread } from './unread'
import './sidebar.css'

// Main navigation inside a workspace. Add a link here when a new workspace section ships.
export function AppSidebar({ workspace }: { workspace: string }) {
  const { openPalette } = useShortcuts()
  const unread = useUnread()
  const count = unread.chats

  // An issue has no section of its own: it stays under the one it was opened from (its project when opened directly).
  const section = useLocation({ select: (l) => l.pathname.slice(workspace.length + 2).split('/')[0] || 'home' })
  const [from, setFrom] = useState(section === 'issue' ? 'projects' : section)
  if (section !== 'issue' && section !== from) setFrom(section)
  const current = (name: string) => (section === 'issue' && from === name ? 'page' : undefined)

  return (
    <nav className="sidebar" aria-label="Workspace">
      <button type="button" className="sidebar-group" aria-keyshortcuts={isApple ? 'Meta+K' : 'Control+K'} onClick={openPalette}>
        <SearchIcon />
        Search
        <kbd className="kbd" style={{ marginLeft: 'auto' }}>{paletteKey}</kbd>
      </button>
      <Link to="/$workspace" params={{ workspace }} activeOptions={{ exact: true }} aria-current={current('home')}>
        <HomeIcon />
        Home
      </Link>
      <Link to="/$workspace/inbox" params={{ workspace }} aria-label={count ? `Inbox, ${count} unread` : undefined} aria-current={current('inbox')}>
        <InboxIcon />
        Inbox
        {count ? <span style={{ marginLeft: 'auto' }}>{count}</span> : null}
      </Link>
      <Link to="/$workspace/my-issues" params={{ workspace }} aria-current={current('my-issues')}>
        <UserIcon />
        My issues
      </Link>
      <Link to="/$workspace/projects" params={{ workspace }} aria-current={current('projects')}>
        <FolderIcon />
        Projects
      </Link>
      <Link to="/$workspace/settings" params={{ workspace }} aria-current={current('settings')}>
        <SettingsIcon />
        Settings
      </Link>
    </nav>
  )
}
