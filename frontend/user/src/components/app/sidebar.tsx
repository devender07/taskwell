import { Link, defaultParseSearch, useLoaderData, useLocation, useRouteContext } from '@tanstack/react-router'
import { BugIcon, FeedbackIcon, FileIcon, FolderIcon, HomeIcon, InboxIcon, LayersIcon, LogOutIcon, NoteIcon, RoadmapIcon, SearchIcon, SettingsIcon, TrashIcon, UserIcon, UsersIcon } from '@taskwell/shared/icons'
import { useState } from 'react'
import { can, featureOn, useFeatures, useFeedbackUrl, type Feature } from '@/api'
import { LockedLink } from './locked-link'
import { useMenu, useSignOut, WorkspaceSwitcher } from './navbar'
import { isApple, paletteKey, useShortcuts } from './shortcuts'
import { useUnread } from './unread'
import './sidebar.css'

// Main navigation inside a workspace. Add a link here when a new workspace section ships.
export function AppSidebar({ workspace }: { workspace: string }) {
  const { openPalette } = useShortcuts()
  const ws = useLoaderData({ from: '/_app/$workspace' })
  const features = useFeatures()
  const feedbackUrl = useFeedbackUrl()
  const on = (name: Feature) => featureOn(features, name)
  const upgrades = useRouteContext({ from: '/_app/$workspace', select: (c) => c.upgrades })
  const locked = (name: Feature, label: string, Icon: typeof HomeIcon) => {
    const plan = upgrades[name]
    return plan ? <LockedLink workspace={workspace} feature={name} plan={plan} Icon={Icon}>{label}</LockedLink> : null
  }
  const team = can(ws, 'analytics.view') && on('team')
  const guest = ws.guest
  const unread = useUnread()
  const views = useLoaderData({ from: '/_app/$workspace/_main' })
  const [open, setOpen] = useMenu()
  const { user, workspaces } = useRouteContext({ from: '/_app' })
  const count = unread.chats
  const [signOutError, signOut, signingOut] = useSignOut()

  // An issue has no section of its own: it stays under the one it was opened from (its project when opened directly).
  const section = useLocation({ select: (l) => l.pathname.slice(workspace.length + 2).split('/')[0] || 'home' })
  const [from, setFrom] = useState(section === 'issue' ? 'projects' : section)
  if (section !== 'issue' && section !== from) setFrom(section)
  const current = (name: string) => (section === 'issue' && from === name ? 'page' : undefined)

  return (
    <nav
      id="app-sidebar"
      className="sidebar"
      aria-label="Workspace"
      data-open={open}
      onClick={(e) => {
        const el = (e.target as Element).closest('a, button')
        if (el && !el.hasAttribute('aria-expanded')) setOpen(false) // a popover trigger keeps the menu open
      }}
      onKeyDown={(e) => e.key === 'Escape' && setOpen(false)}
    >
      <div style={{ marginBottom: 8 }}>
        <WorkspaceSwitcher workspaces={workspaces} canCreate={user.can_create_workspaces} />
      </div>
      <button type="button" className="sidebar-group" aria-keyshortcuts={isApple ? 'Meta+K' : 'Control+K'} onClick={openPalette}>
        <SearchIcon />
        Search
        <kbd className="kbd" style={{ marginLeft: 'auto' }}>{paletteKey}</kbd>
      </button>
      <Link to="/$workspace" params={{ workspace }} activeOptions={{ exact: true }} aria-current={current('home')}>
        <HomeIcon />
        Home
      </Link>
      {guest ? null : !on('chat') ? locked('chat', 'Inbox', InboxIcon) : (
        <Link to="/$workspace/inbox" params={{ workspace }} aria-label={count ? `Inbox, ${count} unread` : undefined} aria-current={current('inbox')}>
          <InboxIcon />
          Inbox
          {count ? <span style={{ marginLeft: 'auto' }}>{count}</span> : null}
        </Link>
      )}
      <Link to="/$workspace/my-issues" params={{ workspace }} aria-current={current('my-issues')}>
        <UserIcon />
        My issues
      </Link>
      <Link to="/$workspace/projects" params={{ workspace }} aria-current={current('projects')}>
        <FolderIcon />
        Projects
      </Link>
      {on('files') ? (
        <Link to="/$workspace/files" params={{ workspace }} aria-current={current('files')}>
          <FileIcon />
          Files
        </Link>
      ) : locked('files', 'Files', FileIcon)}
      {guest ? null : (
        <>
          {on('notes') ? (
            <Link to="/$workspace/notes" params={{ workspace }} aria-current={current('notes')}>
              <NoteIcon />
              Notes
            </Link>
          ) : locked('notes', 'Notes', NoteIcon)}
          {on('roadmap') ? (
            <Link to="/$workspace/roadmap" params={{ workspace }} aria-current={current('roadmap')}>
              <RoadmapIcon />
              Roadmap
            </Link>
          ) : locked('roadmap', 'Roadmap', RoadmapIcon)}
          {on('feedback') ? (
            <Link to="/$workspace/feedback" params={{ workspace }} aria-label={unread.feedback ? `Feedback, ${unread.feedback} new` : undefined} aria-current={current('feedback')}>
              <FeedbackIcon />
              Feedback
              {unread.feedback ? <span style={{ marginLeft: 'auto' }}>{unread.feedback}</span> : null}
            </Link>
          ) : locked('feedback', 'Feedback', FeedbackIcon)}
        </>
      )}
      {team && !guest ? (
        <Link to="/$workspace/team" params={{ workspace }} aria-current={current('team')}>
          <UsersIcon />
          Team
        </Link>
      ) : !guest && can(ws, 'analytics.view') ? locked('team', 'Team', UsersIcon) : null}
      <Link to="/$workspace/trash" params={{ workspace }} aria-current={current('trash')}>
        <TrashIcon />
        Trash
      </Link>
      <Link to="/$workspace/settings" params={{ workspace }} aria-current={current('settings')}>
        <SettingsIcon />
        Settings
      </Link>
      {views.length ? (
        <>
          <h2 className="sidebar-heading">Views</h2>
          {views.map((v) => {
            const [path = '', query = ''] = v.path.split('?')
            return (
              // The saved path is checked by the API against the issue pages; the cast only skips the route-typed `to`.
              <Link key={v.id} to={`/${workspace}${path}` as '/'} search={defaultParseSearch(query)} activeOptions={{ exact: true }}>
                <LayersIcon className="flex-noshrink" />
                <span className="ellipsis">{v.name}</span>
                {v.shared ? (
                  <span className="flex-noshrink" style={{ display: 'inline-flex', marginLeft: 'auto' }} title="Shared with workspace">
                    <UsersIcon size={14} />
                    <span className="sr-only">(shared)</span>
                  </span>
                ) : null}
              </Link>
            )
          })}
        </>
      ) : null}
      <form action={signOut} className="sidebar-end">
        {feedbackUrl ? (
          <a href={feedbackUrl} target="_blank" rel="noreferrer">
            <BugIcon />
            Report a bug
          </a>
        ) : null}
        <button type="submit" className="sidebar-group" disabled={signingOut}>
          <LogOutIcon />
          {signingOut ? 'Signing out…' : 'Sign out'}
        </button>
        {signOutError ? <p role="alert" className="px-3 py-2 text-caption text-accent">{signOutError}</p> : null}
      </form>
    </nav>
  )
}
