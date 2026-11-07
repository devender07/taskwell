import { Link, useLoaderData } from '@tanstack/react-router'
import { BellIcon, CreditCardIcon, FileIcon, FeedbackIcon, FlaskIcon, GridIcon, HistoryIcon, KeyIcon, PlugIcon, ShieldIcon, TagIcon, UserIcon, UsersIcon, WebhookIcon, WorkflowIcon } from '@taskwell/shared/icons'
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
      {ws.guest ? null : (
        <>
          <Link to="/$workspace/settings/workspace" params={{ workspace }}>
            <GridIcon />
            Workspace
          </Link>
          <Link to="/$workspace/settings/billing" params={{ workspace }}>
            <CreditCardIcon />
            Billing
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
          <Link to="/$workspace/settings/templates" params={{ workspace }}>
            <FileIcon />
            Templates
          </Link>
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
          {can(ws, 'audit.view') ? (
            <Link to="/$workspace/settings/audit" params={{ workspace }}>
              <HistoryIcon />
              Audit log
            </Link>
          ) : null}
        </>
      )}
    </nav>
  )
}
