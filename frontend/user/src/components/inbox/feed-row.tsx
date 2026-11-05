import { Link } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { Avatar } from '@taskwell/shared/avatar'
import { fullTime, relativeTime } from '@taskwell/shared/time'
import { avatarSrc, type Notification, type PersonRef } from '@/api'
import { JoinInviteButton } from '@/components/invite/invite-actions'
import './inbox.css'

type FeedRowProps = {
  workspace: string
  identifier?: string
  // A chat notification links to its chat (scrolled to the message) instead of an issue.
  chat?: { id: string; message?: string }
  // A project notification links to the project.
  project?: string
  // An invite notification doesn't link; it has a "Join" button while the invite is available.
  invite?: { id: string; available: boolean }
  actor: PersonRef | null
  // Shown instead of "Someone" when there's no actor (e.g. "GitHub").
  actorName?: string
  text: ReactNode
  excerpt?: string
  at: string
  unread?: boolean
  onClick?: () => void
}

// One line of who did what to which issue or chat, linking to it: the notifications popover and Home's blocks.
export function FeedRow({ workspace, identifier = '', chat, project, invite, actor, actorName, text, excerpt, at, unread, onClick }: FeedRowProps) {
  const name = actor?.name ?? actorName ?? 'Someone'
  const body = (
    <>
      <Avatar name={name} size={24} src={actor && avatarSrc(actor.id)} />
      <span className="grid gap-1 flex-grow" style={{ minWidth: 0 }}>
        <span className="text-caption text-secondary" style={{ overflowWrap: 'anywhere' }}>
          <span className="text-primary">{name}</span> {text}
        </span>
        {excerpt ? <span className="text-caption text-secondary ellipsis">{excerpt}</span> : null}
        {invite && !invite.available ? <span className="text-caption text-secondary">This invite is no longer available.</span> : null}
      </span>
      {invite?.available ? <JoinInviteButton id={invite.id} onJoin={onClick} /> : null}
      <time dateTime={at} title={fullTime(at)} className="text-caption text-secondary flex-noshrink">
        {relativeTime(at)}
      </time>
      {unread !== undefined ? (
        <span className="inbox-dot flex-noshrink" data-unread={unread || undefined} aria-label={unread ? 'Unread' : undefined} role={unread ? 'img' : undefined} />
      ) : null}
    </>
  )
  if (invite) return <div className="inbox-link">{body}</div>
  if (project)
    return (
      <Link to="/$workspace/projects/$key" params={{ workspace, key: project }} className="inbox-link" onClick={onClick}>
        {body}
      </Link>
    )
  if (chat)
    return (
      <Link
        to="/$workspace/inbox/c/$chatId"
        params={{ workspace, chatId: chat.id }}
        search={chat.message ? { message: chat.message } : {}}
        className="inbox-link"
        onClick={onClick}
      >
        {body}
      </Link>
    )
  return (
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
  if (n.kind === 'invite') return <>invited you to join <span className="text-primary font-510">{n.invite.workspace}</span></>
  if (n.kind === 'project_added') return <>added you to <span className="text-primary font-510">{n.project.name}</span></>
  if (n.issue === null) return n.kind === 'chat_mention' ? <>mentioned you in <span className="text-primary font-510">{n.chat.name}</span></> : <>sent you a message</>
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

// Where a notification's row links: its issue, or its chat scrolled to the message.
export const notificationTarget = (n: Notification) =>
  n.kind === 'invite'
    ? { invite: n.invite }
    : n.kind === 'project_added'
      ? { project: n.project.key }
      : n.issue === null
        ? { chat: { id: n.chat.id, message: n.data.message_id } }
        : { identifier: n.issue.identifier }
