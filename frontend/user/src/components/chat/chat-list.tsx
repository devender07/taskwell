import { Link, useNavigate } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { Avatar } from '@taskwell/shared/avatar'
import { LogoMark } from '@taskwell/shared/logo'
import { IconButton } from '@taskwell/shared/icon-button'
import { Popover } from '@taskwell/shared/popover'
import { fullTime, relativeTime } from '@taskwell/shared/time'
import { PlusIcon } from '@taskwell/shared/icons'
import { avatarSrc, type Chat, type NotificationPage, type User } from '@/api'
import { notificationText } from '@/components/inbox/feed-row'
import { PROVIDER_NAME } from '@/components/issues/pull-requests'
import { NewGroupDialog, NewMessageDialog, NewMessagePeople } from './chat-dialogs'
import './chat.css'

type Props = { slug: string; workspaceName: string; user: User; chats: Chat[]; bot: NotificationPage; dialog?: 'group' | 'message' }

// The inbox's left column: the workspace chat, then groups and direct messages, Taskwell's first.
export function ChatList({ slug, workspaceName, user, chats, bot, dialog }: Props) {
  const navigate = useNavigate()
  const top = chats.filter((c) => c.kind === 'workspace')
  const groups = chats.filter((c) => c.kind === 'group')
  const direct = chats.filter((c) => c.kind === 'direct')
  const setDialog = (d?: 'group' | 'message') => navigate({ to: '.', search: (prev: Record<string, unknown>) => ({ ...prev, new: d }), replace: true })

  return (
    <nav className="chat-list" aria-label="Chats">
      <ul className="chat-rows">
        {top.map((c) => (
          <ChatRow key={c.id} slug={slug} chat={c} me={user} workspaceName={workspaceName} />
        ))}
      </ul>
      <Section
        title="Groups"
        add={
          <IconButton aria-label="New group" tooltip="New group" className="chat-icon-button" onClick={() => setDialog('group')}>
            <PlusIcon size={16} />
          </IconButton>
        }
        empty="No groups yet."
        chats={groups}
        slug={slug}
        me={user}
        workspaceName={workspaceName}
      />
      <Section
        title="Direct messages"
        add={
          <Popover
            aria-label="New message"
            placement="bottom-end"
            className="chat-people-popover"
            trigger={(props) => (
              <IconButton aria-label="New message" tooltip="New message" className="chat-icon-button" {...props}>
                <PlusIcon size={16} />
              </IconButton>
            )}
          >
            {(close) => <NewMessagePeople slug={slug} me={user.id} onDone={close} />}
          </Popover>
        }
        pinned={<BotRow slug={slug} page={bot} />}
        chats={direct}
        slug={slug}
        me={user}
        workspaceName={workspaceName}
      />
      <NewGroupDialog open={dialog === 'group'} onClose={() => setDialog()} slug={slug} me={user.id} />
      <NewMessageDialog open={dialog === 'message'} onClose={() => setDialog()} slug={slug} me={user.id} />
    </nav>
  )
}

type SectionProps = { title: string; add: ReactNode; empty?: string; pinned?: ReactNode; chats: Chat[]; slug: string; me: User; workspaceName: string }

function Section({ title, add, empty, pinned, chats, slug, me, workspaceName }: SectionProps) {
  return (
    <section className="grid gap-1" aria-label={title}>
      <div className="chat-list-heading">
        <h2 className="text-label text-secondary m-0">{title}</h2>
        {add}
      </div>
      {chats.length || pinned ? (
        <ul className="chat-rows">
          {pinned}
          {chats.map((c) => (
            <ChatRow key={c.id} slug={slug} chat={c} me={me} workspaceName={workspaceName} />
          ))}
        </ul>
      ) : (
        <p className="text-caption text-secondary m-0 px-3">{empty}</p>
      )}
    </section>
  )
}

// The person or initials a chat shows: the other person in a direct chat.
export function ChatAvatar({ chat, me, workspaceName, size = 24 }: { chat: Chat; me: User; workspaceName: string; size?: number }) {
  if (chat.kind === 'direct') {
    const other = chat.members.find((m) => m.id !== me.id) ?? chat.members[0]
    return <Avatar name={other?.name ?? chat.name} size={size} src={other && avatarSrc(other.id)} />
  }
  return <Avatar name={chat.kind === 'workspace' ? workspaceName : chat.name} size={size} />
}

// Taskwell's direct message: notifications other than chat ones. It isn't a chat, so it can't be left or deleted.
function BotRow({ slug, page }: { slug: string; page: NotificationPage }) {
  const last = page.notifications[0]
  const unread = page.notifications.filter((n) => !n.read).length
  return (
    <li>
      <Link to="/$workspace/inbox/taskwell" params={{ workspace: slug }} className="chat-row" aria-label={unread ? `Taskwell, ${unread} unread` : 'Taskwell'}>
        <LogoMark size={24} />
        <span className="grid flex-grow" style={{ minWidth: 0, gridTemplateColumns: 'minmax(0, 1fr)' }}>
          <span className="flex items-baseline gap-2">
            <span className={`chat-row-name ellipsis flex-grow${unread ? ' font-590' : ''}`}>Taskwell</span>
            {last ? (
              <time dateTime={last.created_at} title={fullTime(last.created_at)} className="text-caption text-secondary flex-noshrink">
                {relativeTime(last.created_at)}
              </time>
            ) : null}
          </span>
          <span className="flex items-center gap-2">
            <span className="text-caption text-secondary ellipsis flex-grow">
              {last ? <>{last.actor?.name ?? (last.data.via && PROVIDER_NAME[last.data.via.provider]) ?? 'Someone'} {notificationText(last)}</> : 'Notifications show up here'}
            </span>
            {unread ? (
              <span className="chat-pill" data-mentioned aria-hidden>
                {page.has_more && unread === page.notifications.length ? `${unread}+` : unread}
              </span>
            ) : null}
          </span>
        </span>
      </Link>
    </li>
  )
}

function ChatRow({ slug, chat, me, workspaceName }: { slug: string; chat: Chat; me: User; workspaceName: string }) {
  const last = chat.last_message
  const unread = chat.unread > 0
  const label = `${chat.name}${unread ? `, ${chat.unread} unread${chat.mentioned ? ', mentions you' : ''}` : ''}`
  return (
    <li>
      <Link to="/$workspace/inbox/c/$chatId" params={{ workspace: slug, chatId: chat.id }} className="chat-row" aria-label={label}>
        <ChatAvatar chat={chat} me={me} workspaceName={workspaceName} />
        <span className="grid flex-grow" style={{ minWidth: 0, gridTemplateColumns: 'minmax(0, 1fr)' }}>
          <span className="flex items-baseline gap-2">
            <span className={`chat-row-name ellipsis flex-grow${unread ? ' font-590' : ''}`}>{chat.name}</span>
            {last ? (
              <time dateTime={last.created_at} title={fullTime(last.created_at)} className="text-caption text-secondary flex-noshrink">
                {relativeTime(last.created_at)}
              </time>
            ) : null}
          </span>
          <span className="flex items-center gap-2">
            <span className="text-caption text-secondary ellipsis flex-grow">
              {last ? `${last.author?.id === me.id ? 'You: ' : last.author && chat.kind !== 'direct' ? `${last.author.name}: ` : ''}${last.excerpt}` : 'No messages yet'}
            </span>
            {unread ? (
              <span className="chat-pill" data-mentioned={chat.mentioned || undefined} aria-hidden>
                {chat.unread > 99 ? '99+' : chat.unread}
              </span>
            ) : null}
          </span>
        </span>
      </Link>
    </li>
  )
}

