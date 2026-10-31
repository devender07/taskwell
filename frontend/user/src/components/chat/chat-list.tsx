import { Link, useNavigate } from '@tanstack/react-router'
import { Avatar } from '@tether/shared/avatar'
import { IconButton } from '@tether/shared/icon-button'
import { fullTime, relativeTime } from '@tether/shared/time'
import { avatarSrc, type Chat, type Member, type User } from '@/api'
import { NewGroupDialog, NewMessageDialog } from './chat-dialogs'
import './chat.css'

type Props = { slug: string; workspaceName: string; user: User; chats: Chat[]; members: Member[]; dialog?: 'group' | 'message' }

// The inbox's left column: Activity, the workspace chat, Notes, then groups and direct messages.
export function ChatList({ slug, workspaceName, user, chats, members, dialog }: Props) {
  const navigate = useNavigate()
  const top = chats.filter((c) => c.kind === 'workspace' || c.kind === 'self')
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
      <Section title="Groups" add="New group" onAdd={() => setDialog('group')} empty="No groups yet." chats={groups} slug={slug} me={user} workspaceName={workspaceName} />
      <Section title="Direct messages" add="New message" onAdd={() => setDialog('message')} empty="No messages yet." chats={direct} slug={slug} me={user} workspaceName={workspaceName} />
      <NewGroupDialog open={dialog === 'group'} onClose={() => setDialog()} slug={slug} me={user.id} members={members} />
      <NewMessageDialog open={dialog === 'message'} onClose={() => setDialog()} slug={slug} me={user.id} members={members} />
    </nav>
  )
}

type SectionProps = { title: string; add: string; onAdd: () => void; empty: string; chats: Chat[]; slug: string; me: User; workspaceName: string }

function Section({ title, add, onAdd, empty, chats, slug, me, workspaceName }: SectionProps) {
  return (
    <section className="grid gap-1" aria-label={title}>
      <div className="chat-list-heading">
        <h2 className="text-label text-secondary m-0">{title}</h2>
        <IconButton aria-label={add} title={add} className="chat-icon-button" onClick={onAdd}>
          <PlusIcon />
        </IconButton>
      </div>
      {chats.length ? (
        <ul className="chat-rows">
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

// The person or initials a chat shows: the other person in a direct chat, you in Notes.
export function ChatAvatar({ chat, me, workspaceName, size = 24 }: { chat: Chat; me: User; workspaceName: string; size?: number }) {
  if (chat.kind === 'direct') {
    const other = chat.members.find((m) => m.id !== me.id) ?? chat.members[0]
    return <Avatar name={other?.name ?? chat.name} size={size} src={other && avatarSrc(other.id)} />
  }
  if (chat.kind === 'self') return <Avatar name={me.name} size={size} src={me.avatar_url} />
  return <Avatar name={chat.kind === 'workspace' ? workspaceName : chat.name} size={size} />
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
              {last ? `${last.author?.id === me.id ? 'You: ' : last.author && chat.kind !== 'direct' ? `${last.author.name}: ` : ''}${last.excerpt}` : chat.kind === 'self' ? 'Only you can see this' : 'No messages yet'}
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

const iconProps = { width: 16, height: 16, viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', strokeWidth: 1.75, strokeLinecap: 'round', strokeLinejoin: 'round', 'aria-hidden': true } as const

function PlusIcon() {
  return (
    <svg {...iconProps}>
      <path d="M12 5v14M5 12h14" />
    </svg>
  )
}
