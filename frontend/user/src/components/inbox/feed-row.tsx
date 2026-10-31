import { Link } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { Avatar } from '@tether/shared/avatar'
import { fullTime, relativeTime } from '@tether/shared/time'
import { avatarSrc, type Notification, type PersonRef } from '@/api'
import './inbox.css'

type FeedRowProps = {
  workspace: string
  identifier: string
  actor: PersonRef | null
  text: ReactNode
  excerpt?: string
  // Earlier updates folded into this row (the inbox shows one row per issue).
  more?: number
  at: string
  unread?: boolean
  onClick?: () => void
  // Open the issue in the inbox's preview pane (keeping the inbox filter) instead of on its own page.
  inbox?: { filter?: 'unread'; selected: boolean }
}

// One line of who did what to which issue, linking to the issue: the inbox and Home's activity and inbox blocks.
export function FeedRow({ workspace, identifier, actor, text, excerpt, more, at, unread, onClick, inbox }: FeedRowProps) {
  const name = actor?.name ?? 'Someone'
  const body = (
    <>
      <Avatar name={name} size={24} src={actor && avatarSrc(actor.id)} />
      <span className="grid gap-1 flex-grow" style={{ minWidth: 0 }}>
        <span className="text-caption text-secondary" style={{ overflowWrap: 'anywhere' }}>
          <span className="text-primary">{name}</span> {text}
        </span>
        {excerpt ? <span className="text-caption text-secondary ellipsis">{excerpt}</span> : null}
        {more ? <span className="text-caption text-secondary">+{more} earlier {more === 1 ? 'update' : 'updates'}</span> : null}
      </span>
      <time dateTime={at} title={fullTime(at)} className="text-caption text-secondary flex-noshrink">
        {relativeTime(at)}
      </time>
      {unread !== undefined ? (
        <span className="inbox-dot flex-noshrink" data-unread={unread || undefined} aria-label={unread ? 'Unread' : undefined} role={unread ? 'img' : undefined} />
      ) : null}
    </>
  )
  return inbox ? (
    <Link
      to="/$workspace/inbox/$identifier"
      params={{ workspace, identifier }}
      search={inbox.filter ? { filter: inbox.filter } : {}}
      className="inbox-link"
      data-selected={inbox.selected || undefined}
      aria-current={inbox.selected ? 'true' : undefined}
      activeProps={{ 'aria-current': inbox.selected ? 'true' : undefined }}
      onClick={onClick}
    >
      {body}
    </Link>
  ) : (
    <Link to="/$workspace/issue/$identifier" params={{ workspace, identifier }} className="inbox-link" onClick={onClick}>
      {body}
    </Link>
  )
}

export function IssueName({ issue }: { issue: { identifier: string; title: string } }) {
  return (
    <>
      <span className="text-primary font-510">{issue.identifier}</span> <span className="text-primary">{issue.title}</span>
    </>
  )
}

export function notificationText(n: Notification): ReactNode {
  const issue = <IssueName issue={n.issue} />
  switch (n.kind) {
    case 'assigned':
      return <>assigned you {issue}</>
    case 'status':
      return (
        <>
          moved {issue} from {n.data.from?.name ?? 'another status'} to {n.data.to?.name ?? n.issue.status.name}
        </>
      )
    case 'comment':
      return <>commented on {issue}</>
    case 'mention':
      return <>mentioned you in {issue}</>
  }
}
