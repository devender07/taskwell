import { Link, useLoaderData, useLocation } from '@tanstack/react-router'
import { Fragment, useState } from 'react'
import { Dialog } from '@taskwell/shared/dialog'
import { BellIcon, ChevronDownIcon, CreditCardIcon, FileIcon, FeedbackIcon, FlaskIcon, GridIcon, HistoryIcon, KeyIcon, PlugIcon, ShieldIcon, TagIcon, UserIcon, UsersIcon, WebhookIcon, WorkflowIcon } from '@taskwell/shared/icons'
import { can, featureOn, useFeatures, type Feature } from '@/api'
import './settings.css'

const FEATURE_OF: Partial<Record<string, Feature>> = {
  '/$workspace/settings/api-tokens': 'api',
  '/$workspace/settings/templates': 'templates',
  '/$workspace/settings/ai-testing': 'ai_testing',
  '/$workspace/settings/integrations': 'integrations',
  '/$workspace/settings/webhooks': 'webhooks',
  '/$workspace/settings/feedback': 'feedback',
}

function useSections() {
  const ws = useLoaderData({ from: '/_app/$workspace' })
  const features = useFeatures()
  const on = (to: string) => !FEATURE_OF[to] || featureOn(features, FEATURE_OF[to])
  const integrations = can(ws, 'integrations.manage')
  const account = [
    { to: '/$workspace/settings/profile', label: 'Profile', Icon: UserIcon },
    { to: '/$workspace/settings/notifications', label: 'Notifications', Icon: BellIcon },
    { to: '/$workspace/settings/api-tokens', label: 'API tokens', Icon: KeyIcon },
  ] as const
  const workspace = ws.guest
    ? []
    : [
        { to: '/$workspace/settings/workspace', label: 'Workspace', Icon: GridIcon },
        { to: '/$workspace/settings/billing', label: 'Billing', Icon: CreditCardIcon },
        { to: '/$workspace/settings/members', label: 'Members', Icon: UsersIcon },
        { to: '/$workspace/settings/roles', label: 'Roles', Icon: ShieldIcon },
        { to: '/$workspace/settings/workflow', label: 'Workflow', Icon: WorkflowIcon },
        { to: '/$workspace/settings/labels', label: 'Labels', Icon: TagIcon },
        { to: '/$workspace/settings/templates', label: 'Templates', Icon: FileIcon },
        { to: '/$workspace/settings/ai-testing', label: 'AI testing', Icon: FlaskIcon },
        ...(integrations
          ? [
              { to: '/$workspace/settings/integrations', label: 'Integrations', Icon: PlugIcon },
              { to: '/$workspace/settings/webhooks', label: 'Webhooks', Icon: WebhookIcon },
            ]
          : []),
        ...(can(ws, 'feedback.manage') ? [{ to: '/$workspace/settings/feedback', label: 'Feedback form', Icon: FeedbackIcon }] : []),
        ...(can(ws, 'audit.view') ? [{ to: '/$workspace/settings/audit', label: 'Audit log', Icon: HistoryIcon }] : []),
      ]
  return [
    { label: 'Account', items: account.filter((i) => on(i.to)) },
    { label: 'Workspace', items: workspace.filter((i) => on(i.to)) },
  ].filter((s) => s.items.length)
}

export function SettingsNav({ workspace, onNavigate }: { workspace: string; onNavigate?: () => void }) {
  return (
    <nav className="settings-nav" aria-label="Settings">
      <h2 className="settings-nav-title">Settings</h2>
      {useSections().map((s) => (
        <Fragment key={s.label}>
          <h3 className="settings-nav-group">{s.label}</h3>
          {s.items.map(({ to, label, Icon }) => (
            <Link key={to} to={to} params={{ workspace }} onClick={onNavigate}>
              <Icon />
              {label}
            </Link>
          ))}
        </Fragment>
      ))}
    </nav>
  )
}

// Phones: the current page's name opens the settings menu as a bottom sheet.
export function SettingsSheet({ workspace }: { workspace: string }) {
  const [open, setOpen] = useState(false)
  const slug = useLocation({ select: (l) => l.pathname.split('/settings/')[1]?.split('/')[0] })
  const current = useSections()
    .flatMap((s) => s.items)
    .find((i) => i.to.endsWith(`/${slug}`))
  return (
    <>
      <button type="button" className="settings-sheet-trigger" aria-haspopup="dialog" onClick={() => setOpen(true)}>
        <span className="text-secondary">Settings</span>
        <span aria-hidden="true">/</span>
        {current?.label}
        <ChevronDownIcon size={16} />
      </button>
      <Dialog open={open} onClose={() => setOpen(false)} title="Settings" sheet>
        <SettingsNav workspace={workspace} onNavigate={() => setOpen(false)} />
      </Dialog>
    </>
  )
}
