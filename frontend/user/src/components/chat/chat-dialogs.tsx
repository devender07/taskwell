import { useNavigate, useRouter } from '@tanstack/react-router'
import { useActionState, useEffect, useRef, useState } from 'react'
import { Avatar } from '@tether/shared/avatar'
import { Button } from '@tether/shared/button'
import { Dialog } from '@tether/shared/dialog'
import { IconButton } from '@tether/shared/icon-button'
import { Input } from '@tether/shared/input'
import { SmartAutocomplete } from '@tether/shared/smart-autocomplete'
import { groupName } from '@tether/shared/validate'
import { api, avatarSrc, toFormErrors, type Chat, type FieldErrors, type Member, type PersonRef } from '@/api'
import { FormError } from '@/components/auth/auth-card'
import { reloadRoutes } from '@/realtime'
import './chat.css'

type State = { errors?: FieldErrors; error?: string | null }

export const INBOX_ROUTES = ['/_app/$workspace/_main/inbox', '/_app/$workspace/_main/inbox/c/$chatId']
export const chatsPath = (slug: string) => `/workspaces/${encodeURIComponent(slug)}/chats`

const options = (members: Member[], exclude: string[]) =>
  members.filter((m) => !exclude.includes(m.id)).map((m) => ({ value: m.id, label: m.name, hint: m.email }))

// Picks several people: each pick adds a chip and clears the field for the next one.
export function PeoplePicker({ members, value, onChange, exclude, error }: { members: Member[]; value: string[]; onChange: (ids: string[]) => void; exclude: string[]; error?: string | null }) {
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
        emptyText="No one else to add"
        options={options(members, [...exclude, ...value])}
        error={error}
        onChange={(o) => {
          if (!o) return
          onChange([...value, o.value])
          setRound((n) => n + 1)
        }}
      />
      {value.length ? (
        <ul className="flex flex-wrap gap-2 m-0 p-0" style={{ listStyle: 'none' }} aria-label="Picked people">
          {value.map((id) => {
            const name = members.find((m) => m.id === id)?.name ?? 'Someone'
            return (
              <li key={id} className="chat-chip">
                <Avatar name={name} size={20} src={avatarSrc(id)} />
                <span className="text-caption">{name}</span>
                <IconButton aria-label={`Remove ${name}`} className="chat-chip-remove" onClick={() => onChange(value.filter((v) => v !== id))}>
                  <CloseIcon />
                </IconButton>
              </li>
            )
          })}
        </ul>
      ) : null}
    </div>
  )
}

export function NewGroupDialog({ open, onClose, slug, me, members }: { open: boolean; onClose: () => void; slug: string; me: string; members: Member[] }) {
  return (
    <Dialog open={open} onClose={onClose} title="New group" description="A private chat for the people you add.">
      <NewGroupForm slug={slug} me={me} members={members} onDone={onClose} />
    </Dialog>
  )
}

function NewGroupForm({ slug, me, members, onDone }: { slug: string; me: string; members: Member[]; onDone: () => void }) {
  const router = useRouter()
  const navigate = useNavigate()
  const [name, setName] = useState('')
  const [ids, setIds] = useState<string[]>([])
  const [{ errors, error }, submit, pending] = useActionState(async (): Promise<State> => {
    const errors = { name: groupName(name), member_ids: ids.length ? null : 'Add at least one person.' }
    if (Object.values(errors).some(Boolean)) return { errors }
    const form = new FormData()
    form.set('name', name.trim())
    for (const id of ids) form.append('member_ids', id)
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
      <PeoplePicker members={members} value={ids} onChange={setIds} exclude={[me]} error={errors?.member_ids} />
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
export function NewMessageDialog({ open, onClose, slug, me, members }: { open: boolean; onClose: () => void; slug: string; me: string; members: Member[] }) {
  const [state, start] = useStartDirect(slug)
  return (
    <Dialog open={open} onClose={onClose} title="New message" description="Pick someone to message directly.">
      <SmartAutocomplete
        label="To"
        placeholder="Type a name"
        emptyText="No one found"
        options={options(members, [me])}
        disabled={state.pending}
        error={state.error}
        onChange={(o) => o && start(o.value).then((ok) => ok && onClose())}
      />
    </Dialog>
  )
}

// The "+" next to Direct messages: the workspace's members, filtered as you type; picking one opens your chat with them.
export function NewMessagePeople({ slug, me, members, onDone }: { slug: string; me: string; members: Member[]; onDone: () => void }) {
  const [state, start] = useStartDirect(slug)
  const [query, setQuery] = useState('')
  const q = query.trim().toLowerCase()
  const people = members.filter((m) => m.id !== me && (!q || m.name.toLowerCase().includes(q) || m.email.toLowerCase().includes(q)))
  return (
    <div className="chat-people">
      <Input aria-label="Search people" placeholder="Search people" value={query} onChange={(e) => setQuery(e.target.value)} />
      {state.error ? <FormError>{state.error}</FormError> : null}
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
        </ul>
      ) : (
        <p className="text-caption text-secondary m-0 px-2 py-2">{members.length > 1 ? 'No one found.' : 'No one else is in this workspace yet.'}</p>
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

export function AddPeopleDialog({ open, onClose, slug, chat, members }: { open: boolean; onClose: () => void; slug: string; chat: Chat; members: Member[] }) {
  return (
    <Dialog open={open} onClose={onClose} title="Add people" description={`They'll see the whole history of ${chat.name}.`}>
      <AddPeopleForm slug={slug} chat={chat} members={members} onDone={onClose} />
    </Dialog>
  )
}

function AddPeopleForm({ slug, chat, members, onDone }: { slug: string; chat: Chat; members: Member[]; onDone: () => void }) {
  const router = useRouter()
  const [ids, setIds] = useState<string[]>([])
  const [{ errors, error }, submit, pending] = useActionState(async (): Promise<State> => {
    if (!ids.length) return { errors: { user_ids: 'Add at least one person.' } }
    const form = new FormData()
    for (const id of ids) form.append('user_ids', id)
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
      <PeoplePicker members={members} value={ids} onChange={setIds} exclude={chat.members.map((m) => m.id)} error={errors?.user_ids} />
      <FormError>{error}</FormError>
      <Button type="submit" disabled={pending} style={{ justifySelf: 'end' }}>
        {pending ? 'Adding…' : 'Add people'}
      </Button>
    </form>
  )
}

// Everyone in a group; its creator can remove people.
export function PeopleDialog({ open, onClose, slug, chat, me }: { open: boolean; onClose: () => void; slug: string; chat: Chat; me: string }) {
  const router = useRouter()
  const [error, setError] = useState<string | null>(null)
  const [removing, setRemoving] = useState<string | null>(null)
  const creator = chat.created_by === me
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
    <Dialog open={open} onClose={onClose} title="People" description={`${chat.members.length} ${chat.members.length === 1 ? 'person' : 'people'} in ${chat.name}.`}>
      <ul className="grid gap-1 m-0 p-0" style={{ listStyle: 'none' }}>
        {chat.members.map((p) => (
          <li key={p.id} className="flex items-center gap-3" style={{ minHeight: 44 }}>
            <Avatar name={p.name} size={24} src={avatarSrc(p.id)} />
            <span className="text-body-sm flex-grow ellipsis">
              {p.name}
              {p.id === me ? <span className="text-secondary"> (you)</span> : null}
              {p.id === chat.created_by ? <span className="text-secondary"> · Created the group</span> : null}
            </span>
            {creator && p.id !== me ? (
              <Button variant="ghost" disabled={removing === p.id} onClick={() => remove(p)} aria-label={`Remove ${p.name}`}>
                {removing === p.id ? 'Removing…' : 'Remove'}
              </Button>
            ) : null}
          </li>
        ))}
      </ul>
      <FormError>{error}</FormError>
    </Dialog>
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
    chat.kind === 'self'
      ? ['Delete all notes?', "Every note here is deleted. This can't be undone.", 'Delete notes']
      : chat.kind === 'group'
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
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden>
      <path d="M18 6 6 18M6 6l12 12" />
    </svg>
  )
}
