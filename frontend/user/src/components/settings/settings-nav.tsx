import { Link, useLoaderData } from '@tanstack/react-router'
import { BellIcon, FeedbackIcon, FlaskIcon, GridIcon, KeyIcon, PlugIcon, ShieldIcon, TagIcon, UploadIcon, UserIcon, UsersIcon, WebhookIcon, WorkflowIcon } from '@taskwell/shared/icons'
import { can } from '@/api'
import './settings.css'

export function SettingsNav({ workspace }: { workspace: string }) {
  const ws = useLoaderData({ from: '/_app/$workspace' })
  const integrations = can(ws, 'integrations.manage')
  return (
    <nav className="settings-nav" aria-label="Settings">
      <h2 className="settings-nav-title">Settings</h2>
      <Link to="/$workspace/settings/profile" params={{ workspace }}>
        <UserIcon />
        Profile
      </Link>
      <Link to="/$workspace/settings/notifications" params={{ workspace }}>
        <BellIcon />
        Notifications
      </Link>
      <Link to="/$workspace/settings/api-tokens" params={{ workspace }}>
        <KeyIcon />
        API tokens
      </Link>
      <Link to="/$workspace/settings/workspace" params={{ workspace }}>
        <GridIcon />
        Workspace
      </Link>
      <Link to="/$workspace/settings/members" params={{ workspace }}>
        <UsersIcon />
        Members
      </Link>
      <Link to="/$workspace/settings/roles" params={{ workspace }}>
        <ShieldIcon />
        Roles
      </Link>
      <Link to="/$workspace/settings/workflow" params={{ workspace }}>
        <WorkflowIcon />
        Workflow
      </Link>
      <Link to="/$workspace/settings/labels" params={{ workspace }}>
        <TagIcon />
        Labels
      </Link>
      {can(ws, 'issues.create') ? (
        <Link to="/$workspace/settings/import" params={{ workspace }}>
          <UploadIcon />
          Import
        </Link>
      ) : null}
      <Link to="/$workspace/settings/ai-testing" params={{ workspace }}>
        <FlaskIcon />
        AI testing
      </Link>
      {integrations ? (
        <Link to="/$workspace/settings/integrations" params={{ workspace }}>
          <PlugIcon />
          Integrations
        </Link>
      ) : null}
      {integrations ? (
        <Link to="/$workspace/settings/webhooks" params={{ workspace }}>
          <WebhookIcon />
          Webhooks
        </Link>
      ) : null}
      {can(ws, 'feedback.manage') ? (
        <Link to="/$workspace/settings/feedback" params={{ workspace }}>
          <FeedbackIcon />
          Feedback form
        </Link>
      ) : null}
    </nav>
  )
}
