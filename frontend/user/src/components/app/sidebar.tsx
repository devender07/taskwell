import { Link, useMatchRoute, useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import './sidebar.css'

// Main navigation inside a workspace. Add a link here when a new workspace section ships.
export function AppSidebar({ workspace }: { workspace: string }) {
  const onSettings = Boolean(useMatchRoute()({ to: '/$workspace/settings', params: { workspace }, fuzzy: true }))
  const navigate = useNavigate()
  const [expanded, setExpanded] = useState(onSettings)
  const [wasOnSettings, setWasOnSettings] = useState(onSettings)
  if (onSettings !== wasOnSettings) {
    setWasOnSettings(onSettings)
    if (onSettings) setExpanded(true)
  }

  function toggleSettings() {
    setExpanded(!expanded)
    if (!expanded) navigate({ to: '/$workspace/settings/profile', params: { workspace } })
  }

  return (
    <nav className="sidebar" aria-label="Workspace">
      <Link to="/$workspace" params={{ workspace }} activeOptions={{ exact: true }}>
        <HomeIcon />
        Home
      </Link>
      <Link to="/$workspace/projects" params={{ workspace }}>
        <FolderIcon />
        Projects
      </Link>
      <button
        type="button"
        className="sidebar-group"
        aria-expanded={expanded}
        aria-controls="sidebar-settings"
        onClick={toggleSettings}
      >
        <SettingsIcon />
        Settings
        <ChevronIcon />
      </button>
      {expanded ? (
        <div id="sidebar-settings" className="sidebar-children">
          <Link to="/$workspace/settings/profile" params={{ workspace }}>
            Profile
          </Link>
          <Link to="/$workspace/settings/workspace" params={{ workspace }}>
            Workspace
          </Link>
          <Link to="/$workspace/settings/members" params={{ workspace }}>
            Members
          </Link>
          <Link to="/$workspace/settings/roles" params={{ workspace }}>
            Roles
          </Link>
          <Link to="/$workspace/settings/workflow" params={{ workspace }}>
            Workflow
          </Link>
          <Link to="/$workspace/settings/labels" params={{ workspace }}>
            Labels
          </Link>
        </div>
      ) : null}
    </nav>
  )
}

const iconProps = { width: 16, height: 16, viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', strokeWidth: 1.75, strokeLinecap: 'round', strokeLinejoin: 'round', 'aria-hidden': true } as const

function HomeIcon() {
  return (
    <svg {...iconProps}>
      <path d="m3 10 9-7 9 7v10a1 1 0 0 1-1 1h-5v-7H9v7H4a1 1 0 0 1-1-1z" />
    </svg>
  )
}

function FolderIcon() {
  return (
    <svg {...iconProps}>
      <path d="M3 6a1 1 0 0 1 1-1h5l2 2h9a1 1 0 0 1 1 1v11a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1z" />
    </svg>
  )
}

function SettingsIcon() {
  return (
    <svg {...iconProps}>
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
    </svg>
  )
}

function ChevronIcon() {
  return (
    <svg {...iconProps} width={14} height={14} className="sidebar-chevron">
      <path d="m9 6 6 6-6 6" />
    </svg>
  )
}
