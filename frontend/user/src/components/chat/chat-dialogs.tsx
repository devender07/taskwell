import { Link, useLoaderData, useNavigate, useRouter } from '@tanstack/react-router'
import { useActionState, useEffect, useRef, useState } from 'react'
import { Avatar } from '@taskwell/shared/avatar'
import { Button } from '@taskwell/shared/button'
import { Dialog } from '@taskwell/shared/dialog'
import { IconButton } from '@taskwell/shared/icon-button'
import { Input } from '@taskwell/shared/input'
import { SmartAutocomplete } from '@taskwell/shared/smart-autocomplete'
import { groupName } from '@taskwell/shared/validate'
import { ArrowLeftIcon, XIcon } from '@taskwell/shared/icons'
import { api, avatarSrc, canViewMember, toFormErrors, type Chat, type FieldErrors, type PersonRef, workspacePath } from '@/api'
import { FormError } from '@/components/auth/auth-card'
import { searchMembers, useMemberPages } from '@/components/workspace/member-pages'
import { reloadRoutes } from '@/realtime'
import '@/components/analytics/analytics.css'
import './chat.css'

type State = { errors?: FieldErrors; error?: string | null }

export const INBOX_ROUTES = ['/_app/$workspace/_main/inbox', '/_app/$workspace/_main/inbox/c/$chatId']
export const chatsPath = (slug: string) => `${workspacePath(slug)}/chats`

// Searches the workspace's members (or only its guests) as you type, leaving out the ids in exclude.
// ponytail: guests are filtered from a 20-member search page, so a guest past it needs a longer name; add a guest filter to /members if that bites.
const loadPeople = (slug: string, exclude: string[], guests?: boolean) => async (q: string, signal: AbortSignal) =>
  (await searchMembers(slug, q, 20, signal))
    .filter((m) => !exclude.includes(m.id) && (!guests || m.guest))
    .map((m) => ({ value: m.id, label: m.name, hint: m.guest ? `${m.email} · Guest` : m.email }))

// Picks several people: each pick adds a chip and clears the field for the next one.
export function PeoplePicker({ slug, value, onChange, exclude, error, guests }: { slug: string; value: PersonRef[]; onChange: (people: PersonRef[]) => void; exclude: string[]; error?: string | null; guests?: boolean }) {
  const [round, setRound] = useState(0)
  const root = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (round) root.current?.querySelector('input')?.focus()
  }, [round])
  return (
    <div ref={root} className="grid gap-3">
      <SmartAutocomplete
        key={round}
        label="People"
        placeholder="Type a name"
        emptyText={guests ? 'No other guests to add' : 'No one else to add'}
        loadOptions={loadPeople(slug, [...exclude, ...value.map((p) => p.id)], guests)}
        error={error}
        onChange={(o) => {
          if (!o) return
          onChange([...value, { id: o.value, name: o.label }])
          setRound((n) => n + 1)
        }}
      />
      {value.length ? (
        <ul className="flex flex-wrap gap-2 m-0 p-0" style={{ listStyle: 'none' }} aria-label="Picked people">
          {value.map((p) => (
            <li key={p.id} className="chat-chip">
              <Avatar name={p.name} size={20} src={avatarSrc(p.id)} />
              <span className="text-caption">{p.name}</span>
              <IconButton aria-label={`Remove ${p.name}`} className="chat-chip-remove" onClick={() => onChange(value.filter((v) => v.id !== p.id))}>
                <CloseIcon />
              </IconButton>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  )
}

export function NewGroupDialog({ open, onClose, slug, me }: { open: boolean; onClose: () => void; slug: string; me: string }) {
  return (
    <Dialog open={open} onClose={onClose} title="New group" description="A private chat for the people you add.">
      <NewGroupForm slug={slug} me={me} onDone={onClose} />
    </Dialog>
  )
}

function NewGroupForm({ slug, me, onDone }: { slug: string; me: string; onDone: () => void }) {
  const router = useRouter()
  const navigate = useNavigate()
  const [name, setName] = useState('')
  const [people, setPeople] = useState<PersonRef[]>([])
  const [{ errors, error }, submit, pending] = useActionState(async (): Promise<State> => {
    const errors = { name: groupName(name), member_ids: people.length ? null : 'Add at least one person.' }
    if (Object.values(errors).some(Boolean)) return { errors }
    const form = new FormData()
    form.set('name', name.trim())
    for (const p of people) form.append('member_ids', p.id)
    let chat: Chat
    try {
      chat = await api<Chat>(chatsPath(slug), form)
    } catch (e) {
      return toFormErrors(e)
    }
    await reloadRoutes(router, INBOX_ROUTES)
    onDone()
    navigate({ to: '/$workspace/inbox/c/$chatId', params: { workspace: slug, chatId: chat.id } })
    return {}
  }, {})
  return (
    <form action={submit} noValidate className="grid gap-5">
      <Input label="Name" placeholder="Design team" autoComplete="off" value={name} onChange={(e) => setName(e.target.value)} error={errors?.name} />
      <PeoplePicker slug={slug} value={people} onChange={setPeople} exclude={[me]} error={errors?.member_ids} />
      <FormError>{error}</FormError>
      <Button type="submit" disabled={pending} style={{ justifySelf: 'end' }}>
        {pending ? 'Creating…' : 'Create group'}
      </Button>
    </form>
  )
}

// Opens (creating if needed) the direct chat with a member and navigates to it.
function useStartDirect(slug: string) {
  const router = useRouter()
  const navigate = useNavigate()
  const [state, setState] = useState<{ error?: string | null; pending?: boolean }>({})
  async function start(userId: string) {
    setState({ pending: true })
    const form = new FormData()
    form.set('user_id', userId)
    try {
      const chat = await api<Chat>(`${chatsPath(slug)}/direct`, form)
      await reloadRoutes(router, INBOX_ROUTES)
      setState({})
      navigate({ to: '/$workspace/inbox/c/$chatId', params: { workspace: slug, chatId: chat.id } })
      return true
    } catch (e) {
      const { errors, error } = toFormErrors(e)
      setState({ error: errors.user_id ?? error })
      return false
    }
  }
  return [state, start] as const
}

// The palette's "New message".
export function NewMessageDialog({ open, onClose, slug, me }: { open: boolean; onClose: () => void; slug: string; me: string }) {
  const [state, start] = useStartDirect(slug)
  return (
    <Dialog open={open} onClose={onClose} title="New message" description="Pick someone to message directly.">
      <SmartAutocomplete
        label="To"
        placeholder="Type a name"
        emptyText="No one found"
        loadOptions={loadPeople(slug, [me])}
        disabled={state.pending}
        error={state.error}
        onChange={(o) => o && start(o.value).then((ok) => ok && onClose())}
      />
    </Dialog>
  )
}

// The "+" next to Direct messages: the workspace's members, searched as you type and paged in as you scroll; picking
// one opens your chat with them.
export function NewMessagePeople({ slug, me, onDone }: { slug: string; me: string; onDone: () => void }) {
  const [state, start] = useStartDirect(slug)
  const [query, setQuery] = useState('')
  const pages = useMemberPages(slug, query.trim())
  const people = pages.members.filter((m) => m.id !== me)
  return (
    <div className="chat-people">
      <Input aria-label="Search people" placeholder="Search people" onDebouncedChange={setQuery} />
      {state.error || pages.error ? <FormError>{state.error ?? pages.error}</FormError> : null}
      {people.length ? (
        <ul className="chat-people-list">
          {people.map((m) => (
            <li key={m.id}>
              <button type="button" className="chat-person" disabled={state.pending} onClick={() => start(m.id).then((ok) => ok && onDone())}>
                <Avatar name={m.name} size={24} src={avatarSrc(m.id)} />
                <span className="grid" style={{ minWidth: 0 }}>
                  <span className="text-body-sm ellipsis">{m.name}</span>
                  <span className="text-caption text-secondary ellipsis">{m.email}</span>
                </span>
              </button>
            </li>
          ))}
          {/* Inside the scrolling list, so the next page loads only when you scroll to its end. */}
          {pages.hasMore ? (
            <li>
              <div ref={pages.end} className="text-caption text-secondary text-center py-2">
                Loading more…
              </div>
            </li>
          ) : null}
        </ul>
      ) : (
        <p className="text-caption text-secondary m-0 px-2 py-2">{!pages.loaded ? 'Loading…' : query.trim() ? 'No one found.' : 'No one else is in this workspace yet.'}</p>
      )}
    </div>
  )
}

export function RenameDialog({ open, onClose, slug, chat }: { open: boolean; onClose: () => void; slug: string; chat: Chat }) {
  return (
    <Dialog open={open} onClose={onClose} title="Rename group">
      <RenameForm slug={slug} chat={chat} onDone={onClose} />
    </Dialog>
  )
}

function RenameForm({ slug, chat, onDone }: { slug: string; chat: Chat; onDone: () => void }) {
  const router = useRouter()
  const [name, setName] = useState(chat.name)
  const [{ errors, error }, submit, pending] = useActionState(async (): Promise<State> => {
    const err = groupName(name)
    if (err) return { errors: { name: err } }
    const form = new FormData()
    form.set('name', name.trim())
    try {
      await api<Chat>(`${chatsPath(slug)}/${encodeURIComponent(chat.id)}`, form)
    } catch (e) {
      return toFormErrors(e)
    }
    await reloadRoutes(router, INBOX_ROUTES)
    onDone()
    return {}
  }, {})
  return (
    <form action={submit} noValidate className="grid gap-5">
      <Input label="Name" autoComplete="off" autoFocus value={name} onChange={(e) => setName(e.target.value)} error={errors?.name} />
      <FormError>{error}</FormError>
      <div className="flex justify-end gap-3">
        <Button variant="ghost" onClick={onDone} disabled={pending}>
          Cancel
        </Button>
        <Button type="submit" disabled={pending}>
          {pending ? 'Saving…' : 'Save'}
        </Button>
      </div>
    </form>
  )
}

export function AddPeopleDialog({ open, onClose, slug, chat }: { open: boolean; onClose: () => void; slug: string; chat: Chat }) {
  return (
    <Dialog open={open} onClose={onClose} title="Add people" description={`They'll see the whole history of ${chat.name}.`}>
      <AddPeopleForm slug={slug} chat={chat} onDone={onClose} />
    </Dialog>
  )
}

function AddPeopleForm({ slug, chat, onDone }: { slug: string; chat: Chat; onDone: () => void }) {
  const router = useRouter()
  const [people, setPeople] = useState<PersonRef[]>([])
  const [{ errors, error }, submit, pending] = useActionState(async (): Promise<State> => {
    if (!people.length) return { errors: { user_ids: 'Add at least one person.' } }
    const form = new FormData()
    for (const p of people) form.append('user_ids', p.id)
    try {
      await api<Chat>(`${chatsPath(slug)}/${encodeURIComponent(chat.id)}/members`, form)
    } catch (e) {
      return toFormErrors(e)
    }
    await reloadRoutes(router, INBOX_ROUTES)
    onDone()
    return {}
  }, {})
  return (
    <form action={submit} noValidate className="grid gap-5">
      <PeoplePicker slug={slug} value={people} onChange={setPeople} exclude={chat.members.map((m) => m.id)} error={errors?.user_ids} />
      <FormError>{error}</FormError>
      <Button type="submit" disabled={pending} style={{ justifySelf: 'end' }}>
        {pending ? 'Adding…' : 'Add people'}
      </Button>
    </form>
  )
}

// A group's people are on the chat already; this stands in for page one so useMemberPages fetches nothing.
const NO_PAGE = { members: [], invites: [], total: 0, has_more: false }

// Everyone in a group, or the workspace for its chat, in place of the messages, searchable by name (and email for the
// workspace, whose members page in from the API as you scroll). A group's creator can remove people.
export function PeoplePanel({ slug, chat, me, onClose }: { slug: string; chat: Chat; me: string; onClose: () => void }) {
  const router = useRouter()
  const ws = useLoaderData({ from: '/_app/$workspace' })
  const [query, setQuery] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [removing, setRemoving] = useState<string | null>(null)
  const creator = chat.kind === 'group' && chat.created_by === me
  const workspace = chat.kind === 'workspace'
  const q = query.trim()
  const pages = useMemberPages(slug, workspace ? q : '', workspace ? undefined : NO_PAGE)
  const people: (PersonRef & { email?: string })[] = workspace ? pages.members : chat.members.filter((p) => p.name.toLowerCase().includes(q.toLowerCase()))
  const total = workspace ? pages.total : chat.members.length

  async function remove(p: PersonRef) {
    setRemoving(p.id)
    setError(null)
    try {
      await api(`${chatsPath(slug)}/${encodeURIComponent(chat.id)}/members/${encodeURIComponent(p.id)}`, undefined, 'DELETE')
      await reloadRoutes(router, INBOX_ROUTES)
    } catch (e) {
      setError(toFormErrors(e).error)
    }
    setRemoving(null)
  }

  return (
    <section className="chat-main" aria-labelledby="chat-people-title">
      <header className="chat-header">
        <IconButton aria-label="Back to chat" className="chat-icon-button chat-back" onClick={onClose}>
          <ArrowLeftIcon />
        </IconButton>
        <div className="grid flex-grow" style={{ minWidth: 0 }}>
          <h2 id="chat-people-title" className="chat-title ellipsis">Members</h2>
          <p className="text-caption text-secondary m-0 ellipsis">{workspace && !pages.loaded ? 'Loading…' : `${total} ${total === 1 ? 'person' : 'people'}${q ? ` matching “${q}”` : ` in ${chat.name}`}`}</p>
        </div>
        <IconButton aria-label="Close members" className="chat-icon-button" onClick={onClose}>
          <CloseIcon />
        </IconButton>
      </header>
      <div className="grid gap-2 px-6 py-4" style={{ alignContent: 'start', minHeight: 0, overflowY: 'auto', overscrollBehavior: 'contain' }}>
        <Input
          type="search"
          aria-label="Search members"
          placeholder={workspace ? 'Search by name or email' : 'Search by name'}
          onDebouncedChange={setQuery}
          autoFocus
        />
        <FormError>{error ?? pages.error}</FormError>
        {people.length === 0 && (!workspace || pages.loaded) ? <p className="text-caption text-secondary m-0 py-2">No one matches “{q}”.</p> : null}
        <ul className="grid gap-1 m-0 p-0" style={{ listStyle: 'none' }}>
          {people.map((p) => (
            <li key={p.id} className="flex items-center gap-3" style={{ minHeight: 44 }}>
              <Avatar name={p.name} size={32} src={avatarSrc(p.id)} />
              <span className="grid flex-grow" style={{ minWidth: 0 }}>
                <span className="text-body-sm ellipsis">
                  {canViewMember(ws, p.id, me) ? (
                    <Link to="/$workspace/members/$id" params={{ workspace: slug, id: p.id }} className="member-link">
                      {p.name}
                    </Link>
                  ) : (
                    p.name
                  )}
                  {p.id === me ? <span className="text-secondary"> (you)</span> : null}
                  {chat.kind === 'group' && p.id === chat.created_by ? <span className="text-secondary"> · Created the group</span> : null}
                </span>
                {p.email ? <span className="text-caption text-secondary ellipsis">{p.email}</span> : null}
              </span>
              {creator && p.id !== me ? (
                <Button variant="ghost" disabled={removing === p.id} onClick={() => remove(p)} aria-label={`Remove ${p.name}`}>
                  {removing === p.id ? 'Removing…' : 'Remove'}
                </Button>
              ) : null}
            </li>
          ))}
        </ul>
        {workspace && pages.hasMore ? <div ref={pages.end} className="text-caption text-secondary text-center py-2">Loading more…</div> : null}
      </div>
    </section>
  )
}

export function LeaveDialog({ open, onClose, slug, chat, me }: { open: boolean; onClose: () => void; slug: string; chat: Chat; me: string }) {
  const router = useRouter()
  const navigate = useNavigate()
  const [state, setState] = useState<{ pending?: boolean; error?: string | null }>({})
  async function leave() {
    setState({ pending: true })
    try {
      await api(`${chatsPath(slug)}/${encodeURIComponent(chat.id)}/members/${encodeURIComponent(me)}`, undefined, 'DELETE')
    } catch (e) {
      setState({ error: toFormErrors(e).error })
      return
    }
    onClose()
    await navigate({ to: '/$workspace/inbox', params: { workspace: slug } })
    await reloadRoutes(router, INBOX_ROUTES)
  }
  const last = chat.members.length <= 1
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="Leave group?"
      description={last ? `You're the last one in ${chat.name}, so leaving deletes it and its messages.` : `You won't see ${chat.name} or get its messages unless someone adds you again.`}
    >
      <FormError>{state.error}</FormError>
      <div className="flex justify-end gap-3">
        <Button variant="ghost" onClick={onClose} disabled={state.pending}>
          Cancel
        </Button>
        <Button variant="danger" onClick={leave} disabled={state.pending}>
          {state.pending ? 'Leaving…' : 'Leave group'}
        </Button>
      </div>
    </Dialog>
  )
}

// Deletes the whole chat for everyone in it (a group: its creator only; the API checks).
export function DeleteChatDialog({ open, onClose, slug, chat }: { open: boolean; onClose: () => void; slug: string; chat: Chat }) {
  const router = useRouter()
  const navigate = useNavigate()
  const [state, setState] = useState<{ pending?: boolean; error?: string | null }>({})
  async function remove() {
    setState({ pending: true })
    try {
      await api(`${chatsPath(slug)}/${encodeURIComponent(chat.id)}`, undefined, 'DELETE')
    } catch (e) {
      setState({ error: toFormErrors(e).error })
      return
    }
    onClose()
    await navigate({ to: '/$workspace/inbox', params: { workspace: slug } })
    await reloadRoutes(router, INBOX_ROUTES)
  }
  const [title, description, action] =
    chat.kind === 'group'
      ? [`Delete ${chat.name}?`, `The group and all its messages are deleted for everyone in it. This can't be undone.`, 'Delete group']
      : ['Delete this chat?', `The chat and all its messages are deleted for both of you. This can't be undone.`, 'Delete chat']
  return (
    <Dialog open={open} onClose={onClose} title={title} description={description}>
      <FormError>{state.error}</FormError>
      <div className="flex justify-end gap-3">
        <Button variant="ghost" onClick={onClose} disabled={state.pending}>
          Cancel
        </Button>
        <Button variant="danger" onClick={remove} disabled={state.pending}>
          {state.pending ? 'Deleting…' : action}
        </Button>
      </div>
    </Dialog>
  )
}

export function CloseIcon() {
  return (
    <XIcon size={16} strokeWidth={2} />
  )
}
