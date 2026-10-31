import { Link, useNavigate, useRouter } from '@tanstack/react-router'
import { createContext, use, useEffect, useEffectEvent, useLayoutEffect, useRef, useState, type KeyboardEvent, type MouseEvent } from 'react'
import { Avatar } from '@tether/shared/avatar'
import { Button } from '@tether/shared/button'
import { Dialog } from '@tether/shared/dialog'
import { IconButton } from '@tether/shared/icon-button'
import { Popover } from '@tether/shared/popover'
import { RichText, type Doc } from '@tether/shared/rich-text'
import { fullTime, relativeTime } from '@tether/shared/time'
import { messageBody } from '@tether/shared/validate'
import { api, avatarSrc, toFormErrors, uploadFile, type Chat, type IssueRef, type Member, type Message, type MessagePage, type Replies, type User } from '@/api'
import { FormError } from '@/components/auth/auth-card'
import { useUnread } from '@/components/app/unread'
import { RichEditor } from '@/components/issues/rich-editor'
import { onTyping, reloadRoutes, sendTyping, subscribePaths } from '@/realtime'
import { firstName, firstUnread, groupMessages, typingText } from './chat'
import { AddPeopleDialog, CloseIcon, INBOX_ROUTES, LeaveDialog, PeopleDialog, RenameDialog, chatsPath } from './chat-dialogs'
import { ChatAvatar } from './chat-list'
import './chat.css'

const EMOJI = ['👍', '❤️', '😂', '🎉', '😮', '😢', '👀', '✅']
const FROM = '/$workspace/inbox/c/$chatId'

type Ctx = {
  slug: string
  chat: Chat
  me: User
  mentions: { id: string; name: string }[]
  issues: (q: string) => Promise<{ id: string; label: string; title: string }[]>
  upload: (file: File) => ReturnType<typeof uploadFile>
  issueHref: (identifier: string) => string
  // After this tab's own change (its events skip this tab): reloads the list and the chat, and bumps `version`.
  refresh: () => Promise<void>
  version: number
  editing: string | null
  setEditing: (id: string | null) => void
}
const ChatContext = createContext<Ctx>(null!)

type Props = { slug: string; workspaceName: string; me: User; chat: Chat; page: MessagePage; members: Member[]; thread?: string; message?: string }

const messagePath = (slug: string, id: string) => `/workspaces/${encodeURIComponent(slug)}/messages/${encodeURIComponent(id)}`
// ProseMirror marks every Esc and some arrow keys handled, so "was this key for an open @ or # list" is asked of the DOM.
const suggesting = () => !!document.querySelector('.rich-editor-mentions')
const nearBottom = (el: HTMLElement) => el.scrollHeight - el.scrollTop - el.clientHeight < 40
const clock = (iso: string) => new Date(iso).toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' })

// One chat: header, messages (newest at the bottom, older pages load on scroll up), typing line and composer, and the
// thread panel when `thread` is set. Keyed by chat id.
export function ChatView({ slug, workspaceName, me, chat, page, members, thread, message }: Props) {
  const router = useRouter()
  const navigate = useNavigate({ from: FROM })
  const { refresh: refreshUnread } = useUnread()
  const path = `${chatsPath(slug)}/${encodeURIComponent(chat.id)}`
  const [loaded, setLoaded] = useState(page)
  const [items, setItems] = useState(page.messages)
  const [hasMore, setHasMore] = useState(page.has_more)
  const [olderError, setOlderError] = useState<string | null>(null)
  const [version, setVersion] = useState(0)
  const [editing, setEditing] = useState<string | null>(null)
  const [newId] = useState(() => firstUnread(page.messages, chat.unread, me.id))
  const [pill, setPill] = useState(false)
  const scroller = useRef<HTMLDivElement>(null)
  const top = useRef<HTMLDivElement>(null)
  const atBottom = useRef(true)
  const prepended = useRef<number | null>(null)
  const loadingOlder = useRef(false)
  const lastId = items.at(-1)?.id
  const shownLast = useRef(lastId)
  const readTimer = useRef<ReturnType<typeof setTimeout>>(undefined)

  // A refetch replaces the newest page and keeps the older pages already loaded.
  // ponytail: older pages don't see edits, reactions or deletes until the chat is reopened.
  if (page !== loaded) {
    const first = page.messages[0]
    const older = page.has_more && first ? items.filter((m) => m.id < first.id) : []
    setLoaded(page)
    setItems([...older, ...page.messages])
    setHasMore(older.length ? hasMore : page.has_more)
  }

  const ctx: Ctx = {
    slug,
    chat,
    me,
    mentions: chat.kind === 'workspace' ? members : chat.kind === 'self' ? [] : chat.members,
    issues: async (q) => {
      const refs = await api<IssueRef[]>(`/workspaces/${encodeURIComponent(slug)}/issues?${new URLSearchParams({ q })}`)
      return refs.map((r) => ({ id: r.identifier, label: r.identifier, title: r.title }))
    },
    upload: (file) => uploadFile(slug, file),
    issueHref: (identifier) => `/${encodeURIComponent(slug)}/issue/${encodeURIComponent(identifier)}`,
    refresh: async () => {
      await reloadRoutes(router, INBOX_ROUTES)
      setVersion((v) => v + 1)
      refreshUnread()
    },
    version,
    editing,
    setEditing,
  }

  // Opening (or following a link to a message): the message, else the first unread one, else the bottom.
  useLayoutEffect(() => {
    const el = scroller.current
    if (!el) return
    const target = message ?? newId
    const node = target ? el.querySelector(`[data-msg="${CSS.escape(target)}"]`) : null
    if (node) node.scrollIntoView({ block: message ? 'center' : 'start' })
    else el.scrollTop = el.scrollHeight
    atBottom.current = nearBottom(el)
  }, [message, newId])

  // Older page prepended: keep what was on screen in place. New message at the end: follow it when already at the
  // bottom (or it's yours), otherwise offer the "New messages" pill.
  useLayoutEffect(() => {
    const el = scroller.current
    if (!el) return
    if (prepended.current != null) {
      el.scrollTop += el.scrollHeight - prepended.current
      prepended.current = null
      return
    }
    if (lastId === shownLast.current) return
    shownLast.current = lastId
    if (atBottom.current || items.at(-1)?.author?.id === me.id) {
      el.scrollTop = el.scrollHeight
      atBottom.current = true
      setPill(false)
    } else setPill(true)
  }, [items, lastId, me.id])

  // Images load after the first scroll; stay pinned to the bottom while they do.
  useEffect(() => {
    const el = scroller.current
    if (!el) return
    const onLoad = () => atBottom.current && (el.scrollTop = el.scrollHeight)
    el.addEventListener('load', onLoad, true)
    return () => el.removeEventListener('load', onLoad, true)
  }, [])

  const loadOlder = useEffectEvent(async () => {
    const first = items[0]
    if (!hasMore || loadingOlder.current || !first) return
    loadingOlder.current = true
    setOlderError(null)
    try {
      const next = await api<MessagePage>(`${path}/messages?before=${encodeURIComponent(first.id)}`)
      prepended.current = scroller.current?.scrollHeight ?? null
      setItems((prev) => [...next.messages, ...prev])
      setHasMore(next.has_more)
    } catch (e) {
      setOlderError(toFormErrors(e).error)
    }
    loadingOlder.current = false
  })
  const firstId = items[0]?.id
  useEffect(() => {
    const el = top.current
    if (!el || !hasMore) return
    const io = new IntersectionObserver((entries) => entries.some((e) => e.isIntersecting) && loadOlder(), { root: scroller.current, rootMargin: '200px 0px 0px 0px' })
    io.observe(el)
    return () => io.disconnect()
  }, [hasMore, firstId])

  // Read up to the newest message once it's on screen in a focused window.
  const markRead = useEffectEvent(() => {
    if (!(chat.unread > 0 || chat.mentioned) || !document.hasFocus() || !atBottom.current) return
    clearTimeout(readTimer.current)
    readTimer.current = setTimeout(async () => {
      await api(`${path}/read`, new FormData()).catch(() => {})
      await reloadRoutes(router, INBOX_ROUTES)
      refreshUnread()
    }, 1000)
  })
  useEffect(() => markRead(), [chat.unread, chat.mentioned, lastId])
  useEffect(() => {
    const onFocus = () => markRead()
    window.addEventListener('focus', onFocus)
    return () => {
      window.removeEventListener('focus', onFocus)
      clearTimeout(readTimer.current)
    }
  }, [])

  function onScroll() {
    const el = scroller.current
    if (!el) return
    atBottom.current = nearBottom(el)
    if (atBottom.current) {
      setPill(false)
      markRead()
    }
  }

  // Closing a thread that covered the chat (narrow screens) brings its root back into view.
  const [openThread, setOpenThread] = useState(thread)
  if (thread !== openThread) setOpenThread(thread)
  const closedThread = useRef<string | undefined>(undefined)
  useLayoutEffect(() => {
    const closed = closedThread.current
    closedThread.current = openThread
    if (!closed || openThread) return
    scroller.current?.querySelector(`[data-msg="${CSS.escape(closed)}"]`)?.scrollIntoView({ block: 'center' })
    composerFocus(document.querySelector('.chat-main .chat-composer'))
  }, [openThread])

  // Esc closes the thread, then leaves the composer.
  const onKey = useEffectEvent((e: globalThis.KeyboardEvent) => {
    if (e.key !== 'Escape' || suggesting() || document.querySelector('dialog[open], .popover')) return
    if (e.target instanceof HTMLElement && e.target.isContentEditable && e.target.closest('.chat-msg')) return // editing a message: Esc cancels that
    if (thread) {
      e.preventDefault()
      navigate({ search: (prev) => ({ ...prev, thread: undefined }) })
    } else if (e.target instanceof HTMLElement && e.target.closest('.chat-composer')) e.target.blur()
  })
  useEffect(() => {
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  // A deleted message only stays as a placeholder while it has replies to hold.
  const rows = groupMessages(items.filter((m) => !m.deleted || m.reply_count > 0))
  const placeholder = chat.kind === 'self' ? 'Write a note' : `Message ${chat.kind === 'direct' ? firstName(chat.name) : chat.name}`

  return (
    <ChatContext value={ctx}>
      <div className="chat-view" data-thread={thread ? '' : undefined}>
        <section className="chat-main" aria-label={chat.name}>
          <ChatHeader workspaceName={workspaceName} members={members} />
          <div className="chat-scroll-wrap">
            <div ref={scroller} className="chat-scroll" onScroll={onScroll} onClick={followIssueLink(navigate)}>
              <div ref={top} aria-hidden style={{ height: 1 }} />
              {hasMore ? <p className="text-caption text-secondary text-center m-0 py-2">{olderError ?? 'Loading older messages…'}</p> : <ChatStart workspaceName={workspaceName} />}
              <ol className="chat-messages" aria-label="Messages">
                {rows.map(({ msg, day, compact }) => (
                  <li key={msg.id}>
                    {day ? <Divider label={day} /> : null}
                    {msg.id === newId ? <Divider label="New" accent /> : null}
                    <MessageItem msg={msg} compact={compact && msg.id !== newId} highlight={msg.id === message} />
                  </li>
                ))}
              </ol>
            </div>
            {pill ? (
              <button
                type="button"
                className="chat-new-pill"
                onClick={() => {
                  scroller.current?.scrollTo({ top: scroller.current.scrollHeight, behavior: 'smooth' })
                  setPill(false)
                }}
              >
                New messages ↓
              </button>
            ) : null}
          </div>
          <Composer thread={null} placeholder={placeholder} editable={items} />
        </section>
        {thread ? <ThreadPanel key={thread} rootId={thread} /> : null}
      </div>
    </ChatContext>
  )
}

// `#ENG-12` links in messages navigate inside the app instead of reloading the page.
const followIssueLink = (navigate: ReturnType<typeof useNavigate>) => (e: MouseEvent) => {
  const a = e.target instanceof Element ? e.target.closest<HTMLAnchorElement>('a[data-type="issue-mention"]') : null
  if (!a || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return
  e.preventDefault()
  navigate({ href: a.getAttribute('href')! })
}

function composerFocus(root: Element | null) {
  root?.querySelector<HTMLElement>('[contenteditable="true"]')?.focus()
}

function ChatStart({ workspaceName }: { workspaceName: string }) {
  const { chat } = use(ChatContext)
  const text =
    chat.kind === 'self'
      ? 'Notes are just for you: drafts, links, reminders.'
      : chat.kind === 'workspace'
        ? `This is the start of ${workspaceName}'s chat. Everyone in the workspace is here.`
        : chat.kind === 'direct'
          ? `This is the start of your conversation with ${chat.name}.`
          : `This is the start of ${chat.name}.`
  return <p className="text-caption text-secondary m-0 px-6 pt-6 pb-2">{text}</p>
}

function Divider({ label, accent }: { label: string; accent?: boolean }) {
  return (
    <div className="chat-divider" role="separator" aria-label={accent ? 'New messages' : label} data-accent={accent || undefined}>
      <span>{label}</span>
    </div>
  )
}

function ChatHeader({ workspaceName, members }: { workspaceName: string; members: Member[] }) {
  const { slug, chat, me } = use(ChatContext)
  const [dialog, setDialog] = useState<'rename' | 'add' | 'people' | 'leave' | null>(null)
  const close = () => setDialog(null)
  const group = chat.kind === 'group'
  const subtitle =
    chat.kind === 'self' ? 'Only you can see this' : chat.kind === 'workspace' ? `Everyone in ${workspaceName}` : group ? `${chat.members.length} people` : null
  const faces = chat.members.slice(0, 5)
  const extra = chat.members.length - faces.length
  return (
    <header className="chat-header">
      <Link to="/$workspace/inbox" params={{ workspace: slug }} className="chat-back text-caption text-secondary">
        ← Chats
      </Link>
      <ChatAvatar chat={chat} me={me} workspaceName={workspaceName} size={32} />
      <div className="grid flex-grow" style={{ minWidth: 0 }}>
        <h1 className="chat-title ellipsis">{chat.name}</h1>
        {subtitle ? <p className="text-caption text-secondary m-0 ellipsis">{subtitle}</p> : null}
      </div>
      {group ? (
        <>
          <button type="button" className="chat-faces" aria-label={`People in ${chat.name}: ${chat.members.length}`} onClick={() => setDialog('people')}>
            {faces.map((p) => (
              <Avatar key={p.id} name={p.name} size={24} src={avatarSrc(p.id)} />
            ))}
            {extra > 0 ? <span className="text-caption text-secondary">+{extra}</span> : null}
          </button>
          <Popover
            aria-label="Group actions"
            placement="bottom-end"
            trigger={(props) => (
              <IconButton aria-label="Group actions" className="chat-icon-button" {...props}>
                <DotsIcon />
              </IconButton>
            )}
          >
            {(closeMenu) => (
              <div style={{ width: 200 }}>
                {(
                  [
                    ['rename', 'Rename group'],
                    ['add', 'Add people'],
                    ['people', 'People'],
                    ['leave', 'Leave group'],
                  ] as const
                ).map(([key, label]) => (
                  <button
                    key={key}
                    type="button"
                    className="popover-item"
                    onClick={() => {
                      closeMenu()
                      setDialog(key)
                    }}
                  >
                    {label}
                  </button>
                ))}
              </div>
            )}
          </Popover>
          <RenameDialog open={dialog === 'rename'} onClose={close} slug={slug} chat={chat} />
          <AddPeopleDialog open={dialog === 'add'} onClose={close} slug={slug} chat={chat} members={members} />
          <PeopleDialog open={dialog === 'people'} onClose={close} slug={slug} chat={chat} me={me.id} />
          <LeaveDialog open={dialog === 'leave'} onClose={close} slug={slug} chat={chat} me={me.id} />
        </>
      ) : null}
    </header>
  )
}

function MessageItem({ msg, compact, highlight, inThread }: { msg: Message; compact: boolean; highlight?: boolean; inThread?: boolean }) {
  const ctx = use(ChatContext)
  const navigate = useNavigate({ from: FROM })
  const [deleting, setDeleting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const editing = ctx.editing === msg.id
  const name = msg.author?.name ?? 'Someone'
  const openThread = () => navigate({ search: (prev) => ({ ...prev, thread: msg.id, message: undefined }) })

  async function react(emoji: string, mine: boolean) {
    setError(null)
    const path = `${messagePath(ctx.slug, msg.id)}/reactions`
    try {
      if (mine) await api(`${path}?${new URLSearchParams({ emoji })}`, undefined, 'DELETE')
      else {
        const form = new FormData()
        form.set('emoji', emoji)
        await api(path, form)
      }
    } catch (e) {
      const { errors, error } = toFormErrors(e)
      setError(errors.emoji ?? error)
      return
    }
    await ctx.refresh()
  }

  return (
    <article className="chat-msg" data-msg={msg.id} data-compact={compact || undefined} data-highlight={highlight || undefined} aria-label={`Message from ${name}`}>
      <div className="chat-msg-gutter">
        {compact ? (
          <time dateTime={msg.created_at} title={fullTime(msg.created_at)} className="chat-msg-hover-time text-caption text-secondary">
            {clock(msg.created_at)}
          </time>
        ) : (
          <Avatar name={name} size={32} src={msg.author && avatarSrc(msg.author.id)} />
        )}
      </div>
      <div className="grid gap-1" style={{ minWidth: 0 }}>
        {compact ? null : (
          <header className="flex items-baseline gap-2">
            <span className="text-label text-primary ellipsis">{name}</span>
            <time dateTime={msg.created_at} title={fullTime(msg.created_at)} className="text-caption text-secondary flex-noshrink">
              {clock(msg.created_at)}
            </time>
          </header>
        )}
        {editing ? (
          <EditMessage msg={msg} />
        ) : msg.deleted || !msg.body ? (
          <p className="text-body-sm text-secondary m-0">This message was deleted.</p>
        ) : (
          <div className="chat-body">
            <RichText doc={msg.body} issueHref={ctx.issueHref} />
            {msg.edited_at ? (
              <span className="text-caption text-secondary" title={fullTime(msg.edited_at)}>
                (edited)
              </span>
            ) : null}
          </div>
        )}
        {msg.reactions.length ? (
          <div className="flex flex-wrap items-center gap-1">
            {msg.reactions.map((r) => (
              <button
                key={r.emoji}
                type="button"
                className="chat-reaction"
                aria-pressed={r.mine}
                aria-label={`${r.emoji} ${r.count}`}
                title={r.people.join(', ')}
                onClick={() => react(r.emoji, r.mine)}
              >
                <span aria-hidden>{r.emoji}</span>
                <span className="text-caption">{r.count}</span>
              </button>
            ))}
          </div>
        ) : null}
        {!inThread && msg.reply_count > 0 ? (
          <Link from={FROM} to="." search={(prev) => ({ ...prev, thread: msg.id, message: undefined })} className="chat-thread-link">
            <span className="flex items-center">
              {msg.repliers.map((p) => (
                <Avatar key={p.id} name={p.name} size={20} src={avatarSrc(p.id)} />
              ))}
            </span>
            <span className="text-caption text-primary font-510">
              {msg.reply_count} {msg.reply_count === 1 ? 'reply' : 'replies'}
            </span>
            {msg.last_reply_at ? <span className="text-caption text-secondary">· last reply {relativeTime(msg.last_reply_at)}</span> : null}
          </Link>
        ) : null}
        {error ? <p role="alert" className="text-caption text-accent m-0">{error}</p> : null}
      </div>
      {!msg.deleted && !editing ? (
        <div className="chat-msg-tools" role="toolbar" aria-label="Message actions">
          <Popover
            aria-label="Add reaction"
            placement="bottom-end"
            trigger={(props) => (
              <IconButton aria-label="Add reaction" title="Add reaction" className="chat-tool chat-hover-only" {...props}>
                <SmileIcon />
              </IconButton>
            )}
          >
            {(close) => <EmojiPicker onPick={(e) => (close(), react(e, msg.reactions.some((r) => r.emoji === e && r.mine)))} />}
          </Popover>
          {inThread ? null : (
            <IconButton aria-label="Reply in thread" title="Reply in thread" className="chat-tool chat-hover-only" onClick={openThread}>
              <ReplyIcon />
            </IconButton>
          )}
          <Popover
            aria-label="More actions"
            placement="bottom-end"
            trigger={(props) => (
              <IconButton aria-label="More actions" title="More actions" className="chat-tool" {...props}>
                <DotsIcon />
              </IconButton>
            )}
          >
            {(close) => (
              <div style={{ width: 216 }}>
                <EmojiPicker onPick={(e) => (close(), react(e, msg.reactions.some((r) => r.emoji === e && r.mine)))} />
                <hr className="popover-divider" />
                {inThread ? null : (
                  <button type="button" className="popover-item" onClick={() => (close(), openThread())}>
                    Reply in thread
                  </button>
                )}
                {msg.can_edit ? (
                  <button type="button" className="popover-item" onClick={() => (close(), ctx.setEditing(msg.id))}>
                    Edit
                  </button>
                ) : null}
                {msg.can_delete ? (
                  <button type="button" className="popover-item" onClick={() => (close(), setDeleting(true))}>
                    Delete
                  </button>
                ) : null}
              </div>
            )}
          </Popover>
        </div>
      ) : null}
      {msg.can_delete ? <DeleteMessage msg={msg} open={deleting} onClose={() => setDeleting(false)} /> : null}
    </article>
  )
}

function EmojiPicker({ onPick }: { onPick: (emoji: string) => void }) {
  return (
    <div className="chat-emoji-row" role="group" aria-label="Reactions">
      {EMOJI.map((e) => (
        <button key={e} type="button" className="chat-emoji" aria-label={`React with ${e}`} onClick={() => onPick(e)}>
          {e}
        </button>
      ))}
    </div>
  )
}

function DeleteMessage({ msg, open, onClose }: { msg: Message; open: boolean; onClose: () => void }) {
  const ctx = use(ChatContext)
  const [state, setState] = useState<{ pending?: boolean; error?: string | null }>({})
  async function remove() {
    setState({ pending: true })
    try {
      await api(messagePath(ctx.slug, msg.id), undefined, 'DELETE')
    } catch (e) {
      setState({ error: toFormErrors(e).error })
      return
    }
    await ctx.refresh()
    setState({})
    onClose()
  }
  return (
    <Dialog open={open} onClose={onClose} title="Delete message?" description={msg.reply_count ? 'Its thread replies stay.' : "This can't be undone."}>
      <FormError>{state.error}</FormError>
      <div className="flex justify-end gap-3">
        <Button variant="ghost" onClick={onClose} disabled={state.pending}>
          Cancel
        </Button>
        <Button variant="danger" onClick={remove} disabled={state.pending}>
          {state.pending ? 'Deleting…' : 'Delete'}
        </Button>
      </div>
    </Dialog>
  )
}

function EditMessage({ msg }: { msg: Message }) {
  const ctx = use(ChatContext)
  const [draft, setDraft] = useState<{ doc: Doc | null; length: number }>({ doc: msg.body, length: 0 })
  const [state, setState] = useState<{ pending?: boolean; body?: string | null; error?: string | null }>({})
  const root = useRef<HTMLDivElement>(null)
  const latest = useRef(draft)
  latest.current = draft

  useEffect(() => {
    root.current?.scrollIntoView({ block: 'nearest' })
    const t = setTimeout(() => {
      const el = root.current?.querySelector<HTMLElement>('[contenteditable="true"]')
      if (!el) return
      el.focus()
      getSelection()?.selectAllChildren(el)
      getSelection()?.collapseToEnd()
    }, 50)
    return () => clearTimeout(t)
  }, [])

  async function save() {
    const { doc, length } = latest.current
    const body = messageBody(!doc, length)
    if (body) return setState({ body })
    setState({ pending: true })
    const form = new FormData()
    form.set('body', JSON.stringify(doc))
    try {
      await api<Message>(messagePath(ctx.slug, msg.id), form)
    } catch (e) {
      const { errors, error } = toFormErrors(e)
      return setState({ body: errors.body, error })
    }
    await ctx.refresh()
    ctx.setEditing(null)
  }

  function onKeyDown(e: KeyboardEvent) {
    if (e.key !== 'Escape' || suggesting()) return
    ctx.setEditing(null)
  }

  return (
    <div ref={root} className="grid gap-2" onKeyDown={onKeyDown}>
      <RichEditor
        aria-label="Edit message"
        content={msg.body}
        minHeight={44}
        mentions={ctx.mentions}
        issues={ctx.issues}
        upload={ctx.upload}
        error={state.body}
        onChange={(doc, length) => setDraft({ doc, length })}
        onSubmit={save}
      />
      <FormError>{state.error}</FormError>
      <div className="flex items-center justify-end gap-3 flex-wrap">
        <span className="text-caption text-secondary">Enter to save, Esc to cancel</span>
        <Button variant="ghost" onClick={() => ctx.setEditing(null)} disabled={state.pending}>
          Cancel
        </Button>
        <Button onClick={save} disabled={state.pending}>
          {state.pending ? 'Saving…' : 'Save'}
        </Button>
      </div>
    </div>
  )
}

const TYPING_EVERY_MS = 3000
const TYPING_SHOWN_MS = 5000

// Others typing in this chat (or this thread), cleared 5s after their last frame.
function useTyping(chatId: string, thread: string | null, me: string) {
  const [typing, setTyping] = useState<Record<string, { name: string; until: number }>>({})
  useEffect(
    () =>
      onTyping((t) => {
        if (t.chat !== chatId || (t.thread ?? null) !== thread || t.user.id === me) return
        setTyping((prev) => ({ ...prev, [t.user.id]: { name: t.user.name, until: Date.now() + TYPING_SHOWN_MS } }))
      }),
    [chatId, thread, me],
  )
  const any = Object.keys(typing).length > 0
  useEffect(() => {
    if (!any) return
    const t = setInterval(() => {
      const now = Date.now()
      setTyping((prev) => Object.fromEntries(Object.entries(prev).filter(([, v]) => v.until > now)))
    }, 1000)
    return () => clearInterval(t)
  }, [any])
  return typingText(Object.values(typing).map((v) => firstName(v.name)))
}

// The message box: Enter sends, Shift+Enter starts a new line, ↑ in an empty box edits your last message.
function Composer({ thread, placeholder, editable }: { thread: string | null; placeholder: string; editable: Message[] }) {
  const ctx = use(ChatContext)
  const [draft, setDraft] = useState<{ doc: Doc | null; length: number }>({ doc: null, length: 0 })
  const [round, setRound] = useState(0)
  const [state, setState] = useState<{ pending?: boolean; body?: string | null; error?: string | null }>({})
  const root = useRef<HTMLDivElement>(null)
  const lastTyping = useRef(0)
  const latest = useRef({ draft, pending: state.pending })
  latest.current = { draft, pending: state.pending }
  const typing = useTyping(ctx.chat.id, thread, ctx.me.id)

  useEffect(() => {
    if (round) requestAnimationFrame(() => composerFocus(root.current))
  }, [round])

  async function send() {
    const { draft, pending } = latest.current
    if (pending) return
    const body = messageBody(!draft.doc, draft.length)
    if (body) return setState({ body: draft.doc ? body : null })
    setState({ pending: true })
    const form = new FormData()
    form.set('body', JSON.stringify(draft.doc))
    if (thread) form.set('parent_id', thread)
    try {
      await api<Message>(`${chatsPath(ctx.slug)}/${encodeURIComponent(ctx.chat.id)}/messages`, form)
    } catch (e) {
      const { errors, error } = toFormErrors(e)
      return setState({ body: errors.body ?? errors.parent_id, error })
    }
    lastTyping.current = 0 // the next message starts a new typing burst
    setDraft({ doc: null, length: 0 })
    setRound((n) => n + 1)
    setState({})
    await ctx.refresh()
  }

  function onChange(doc: Doc | null, length: number) {
    setDraft({ doc, length })
    const now = Date.now()
    if (ctx.chat.kind !== 'self' && root.current?.contains(document.activeElement) && now - lastTyping.current > TYPING_EVERY_MS) {
      lastTyping.current = now
      sendTyping(ctx.chat.id, thread)
    }
  }

  function onKeyDown(e: KeyboardEvent) {
    if (e.key !== 'ArrowUp' || suggesting() || e.shiftKey || e.altKey || e.metaKey || e.ctrlKey || latest.current.draft.doc) return
    const mine = editable.findLast((m) => m.can_edit)
    if (!mine) return
    e.preventDefault()
    ctx.setEditing(mine.id)
  }

  return (
    <div className="chat-composer" ref={root} onKeyDown={onKeyDown} aria-busy={state.pending || undefined}>
      <p className="chat-typing text-caption text-secondary m-0" aria-live="polite">
        {typing}
      </p>
      <div className="flex items-end gap-2">
        <div className="flex-grow" style={{ minWidth: 0 }}>
          <RichEditor
            key={`${round}:${placeholder}`}
            aria-label={thread ? 'Reply' : 'Message'}
            placeholder={placeholder}
            minHeight={44}
            mentions={ctx.mentions}
            issues={ctx.issues}
            upload={ctx.upload}
            error={state.body}
            onChange={onChange}
            onSubmit={send}
          />
        </div>
        <IconButton aria-label={thread ? 'Send reply' : 'Send message'} title="Send" className="chat-send" disabled={state.pending || !draft.doc} onClick={send}>
          <SendIcon />
        </IconButton>
      </div>
      <FormError>{state.error}</FormError>
    </div>
  )
}

function ThreadPanel({ rootId }: { rootId: string }) {
  const ctx = use(ChatContext)
  const navigate = useNavigate({ from: FROM })
  const [data, setData] = useState<Replies | null>(null)
  const [older, setOlder] = useState<{ replies: Replies['replies']; has_more: boolean } | null>(null)
  const [error, setError] = useState<string | null>(null)
  const heading = useRef<HTMLHeadingElement>(null)
  const scroller = useRef<HTMLDivElement>(null)
  const path = `${messagePath(ctx.slug, rootId)}/replies`

  const load = useEffectEvent(() =>
    api<Replies>(path).then(
      (d) => {
        setData(d)
        setError(null)
      },
      (e) => setError(toFormErrors(e).error),
    ),
  )
  useEffect(() => {
    void load()
    return subscribePaths([path], () => void load())
  }, [path, ctx.version])

  useEffect(() => heading.current?.focus(), [])

  const replies = data ? [...(older?.replies.filter((r) => !data.replies[0] || r.id < data.replies[0].id) ?? []), ...data.replies] : []
  const hasMore = older ? older.has_more : data?.has_more
  const lastReply = replies.at(-1)?.id

  // Follow new replies to the bottom.
  useLayoutEffect(() => {
    const el = scroller.current
    if (el && lastReply) el.scrollTop = el.scrollHeight
  }, [lastReply])

  async function showOlder() {
    const first = replies[0]
    if (!first) return
    try {
      const page = await api<Replies>(`${path}?before=${encodeURIComponent(first.id)}`)
      setOlder({ replies: [...page.replies, ...replies], has_more: page.has_more })
    } catch (e) {
      setError(toFormErrors(e).error)
    }
  }

  const close = () => navigate({ search: (prev) => ({ ...prev, thread: undefined }) })
  const rows = groupMessages(replies.filter((m) => !m.deleted))

  return (
    <section className="chat-thread" aria-labelledby="chat-thread-title">
      <header className="chat-header">
        <button type="button" className="chat-back chat-back-button text-caption text-secondary" onClick={close}>
          ← Back to chat
        </button>
        <h2 id="chat-thread-title" ref={heading} tabIndex={-1} className="chat-title flex-grow">
          Thread
        </h2>
        <IconButton aria-label="Close thread" title="Close" className="chat-icon-button" onClick={close}>
          <CloseIcon />
        </IconButton>
      </header>
      <div ref={scroller} className="chat-scroll" onClick={followIssueLink(navigate)}>
        {error ? <p role="alert" className="text-caption text-accent m-0 px-6 py-4">{error}</p> : null}
        {!data && !error ? <p className="text-caption text-secondary m-0 px-6 py-4">Loading…</p> : null}
        {data ? (
          <>
            <div className="chat-thread-root">
              <MessageItem msg={data.root} compact={false} inThread />
            </div>
            <Divider label={`${data.root.reply_count} ${data.root.reply_count === 1 ? 'reply' : 'replies'}`} />
            {hasMore ? (
              <div className="flex justify-center py-2">
                <Button variant="ghost" onClick={showOlder}>
                  Show older replies
                </Button>
              </div>
            ) : null}
            <ol className="chat-messages" aria-label="Replies">
              {rows.map(({ msg, compact }) => (
                <li key={msg.id}>
                  <MessageItem msg={msg} compact={compact} inThread />
                </li>
              ))}
            </ol>
          </>
        ) : null}
      </div>
      {data ? <Composer thread={rootId} placeholder="Reply…" editable={replies} /> : null}
    </section>
  )
}

const iconProps = { width: 18, height: 18, viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', strokeWidth: 1.75, strokeLinecap: 'round', strokeLinejoin: 'round', 'aria-hidden': true } as const

function DotsIcon() {
  return (
    <svg {...iconProps} fill="currentColor" stroke="none">
      <circle cx={5} cy={12} r={1.75} />
      <circle cx={12} cy={12} r={1.75} />
      <circle cx={19} cy={12} r={1.75} />
    </svg>
  )
}

function SmileIcon() {
  return (
    <svg {...iconProps}>
      <circle cx="12" cy="12" r="9" />
      <path d="M8 14s1.5 2 4 2 4-2 4-2M9 9h.01M15 9h.01" />
    </svg>
  )
}

function ReplyIcon() {
  return (
    <svg {...iconProps}>
      <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />
    </svg>
  )
}

function SendIcon() {
  return (
    <svg {...iconProps}>
      <path d="m22 2-7 20-4-9-9-4z" />
      <path d="M22 2 11 13" />
    </svg>
  )
}
