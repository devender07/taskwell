import { useNavigate, useRouter } from '@tanstack/react-router'
import { useEffect, useId, useRef, useState, type KeyboardEvent, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { filterOptions, type Option } from '@taskwell/shared/match'
import { CheckIcon } from '@taskwell/shared/icons'
import { api, can, listCycles, toFormErrors, type Chat, type Cycle, type Issue, type Label, type Member, type SearchResult, type Status, type Workspace, workspacePath } from '@/api'
import { LabelDot, StatusIcon, assigneeOptions, priorityOptions, statusOptions } from '@/components/issues/fields'
import { reloadPage, subscribePaths } from '@/realtime'
import { peopleSearch } from '@/components/workspace/member-pages'
import './shortcuts.css'

export type Mode = 'root' | 'status' | 'assignee' | 'priority' | 'labels'
export type IssueContext = { issue: Issue; statuses: Status[]; labels: Label[] }

type Props = {
  workspace: Workspace
  mode: Mode | null
  issue?: IssueContext
  projectKey?: string
  copyBranch?: { run: () => void; key: string }
  onCreate?: () => void
  onClose: () => void
}
type Item = { id: string; label: string; lead?: ReactNode; hint?: ReactNode; checked?: boolean; run: () => void }
type Group = { label: string; items: Item[] }

const MODE_LABEL: Record<Exclude<Mode, 'root'>, string> = { status: 'Status', assignee: 'Assignee', priority: 'Priority', labels: 'Labels' }
const MODE_ACTION: Record<Exclude<Mode, 'root'>, [string, string]> = {
  status: ['Change status…', 'S'],
  assignee: ['Assign to…', 'A'],
  priority: ['Set priority…', 'P'],
  labels: ['Change labels…', 'L'],
}
const SETTINGS = ['Profile', 'Notifications', 'API tokens', 'Workspace', 'Members', 'Roles', 'Workflow', 'Labels']

export function CommandPalette({ mode, onClose, ...rest }: Props) {
  const ref = useRef<HTMLDialogElement>(null)

  useEffect(() => {
    const dialog = ref.current
    if (!dialog) return
    if (mode && !dialog.open) {
      dialog.showModal()
      dialog.querySelector('input')?.focus()
    } else if (!mode && dialog.open) dialog.close()
  }, [mode])

  return createPortal(
    <dialog
      ref={ref}
      className="dialog palette"
      aria-label="Command palette"
      onCancel={(e) => {
        e.preventDefault()
        onClose()
      }}
      onClick={(e) => e.target === e.currentTarget && onClose()}
    >
      {mode ? <PaletteBody initial={mode} onClose={onClose} {...rest} /> : null}
    </dialog>,
    document.body,
  )
}

function PaletteBody({ workspace, initial, issue: ctx, projectKey, copyBranch, onCreate, onClose }: Omit<Props, 'mode'> & { initial: Mode }) {
  const id = useId()
  const navigate = useNavigate()
  const router = useRouter()
  const [mode, setMode] = useState<Mode>(initial)
  const [query, setQuery] = useState('')
  const [active, setActive] = useState(0)
  const [found, setFound] = useState<{ q: string; data: SearchResult } | null>(null)
  const [queued, setQueued] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [version, setVersion] = useState(0)
  const [labelIds, setLabelIds] = useState(() => ctx?.issue.labels.map((l) => l.id) ?? [])
  const [chats, setChats] = useState<Chat[]>([])
  const [cycles, setCycles] = useState<{ key: string; list: Cycle[] } | null>(null)
  const [people, setPeople] = useState<{ q: string; list: Member[] } | null>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const q = query.trim()
  const slug = workspace.slug

  useEffect(() => {
    if (mode !== 'root') return
    const ws = `${workspacePath(slug)}`
    return subscribePaths([`${ws}/search`, `${ws}/issues/`, `${ws}/projects`, `${ws}/projects/`], () => setVersion((v) => v + 1))
  }, [mode, slug])

  useEffect(() => {
    if (mode !== 'root') return
    const ctrl = new AbortController()
    const t = setTimeout(() => {
      api<SearchResult>(`${workspacePath(slug)}/search?q=${encodeURIComponent(q)}`, undefined, 'GET', ctrl.signal).then(
        (data) => setFound({ q, data }),
        () => !ctrl.signal.aborted && setFound({ q, data: { issues: [], projects: [] } }),
      )
    }, 120)
    return () => {
      clearTimeout(t)
      ctrl.abort()
    }
  }, [mode, q, slug, version])

  useEffect(() => {
    if (mode !== 'assignee' || !ctx) return
    const ctrl = new AbortController()
    const t = setTimeout(() => {
      peopleSearch(slug, ctx.issue.project.key)(q, ctrl.signal).then((list) => setPeople({ q, list }), () => {})
    }, 120)
    return () => {
      clearTimeout(t)
      ctrl.abort()
    }
  }, [mode, q, slug, ctx])

  useEffect(() => {
    if (mode !== 'root') return
    const ctrl = new AbortController()
    api<Chat[]>(`${workspacePath(slug)}/chats`, undefined, 'GET', ctrl.signal).then(setChats, () => {})
    return () => ctrl.abort()
  }, [mode, slug])

  useEffect(() => {
    if (mode !== 'root' || !projectKey) return
    const ctrl = new AbortController()
    listCycles(slug, projectKey, ctrl.signal).then((list) => setCycles({ key: projectKey, list }), () => {})
    return () => ctrl.abort()
  }, [mode, slug, projectKey])

  async function save(field: string, values: string[], stay = false) {
    if (!ctx) return
    const form = new FormData()
    if (values.length === 0) form.set(field, '')
    for (const v of values) form.append(field, v)
    setError(null)
    try {
      await api<Issue>(`${workspacePath(slug)}/issues/${encodeURIComponent(ctx.issue.identifier)}`, form)
    } catch (e) {
      const { errors, error } = toFormErrors(e)
      setError(Object.values(errors).find(Boolean) ?? error)
      return false
    }
    if (!stay) onClose()
    reloadPage(router)
    return true
  }

  function go(path: string) {
    onClose()
    navigate({ href: `/${encodeURIComponent(slug)}/${path}` })
  }

  function enter(next: Mode) {
    setMode(next)
    setQuery('')
    setActive(0)
    setError(null)
  }

  const groups: Group[] = []
  if (mode === 'root') {
    const data = found?.data
    if (data?.issues.length) {
      groups.push({
        label: q ? 'Issues' : 'Recent',
        items: data.issues.map((i) => ({
          id: `issue-${i.id}`,
          label: i.title,
          lead: (
            <>
              <StatusIcon status={i.status} />
              <span className="issue-id flex-noshrink">{i.identifier}</span>
            </>
          ),
          hint: i.status.name,
          run: () => go(`issue/${encodeURIComponent(i.identifier)}`),
        })),
      })
    }
    if (data?.projects.length) {
      groups.push({
        label: 'Projects',
        items: data.projects.map((p) => ({
          id: `project-${p.key}`,
          label: p.name,
          lead: <span className="issue-id flex-noshrink">{p.key}</span>,
          hint: p.archived ? 'Archived' : undefined,
          run: () => go(`projects/${encodeURIComponent(p.key)}`),
        })),
      })
    }
    const actions: (Option & { run: () => void })[] = []
    if (onCreate) actions.push({ value: 'create', label: 'Create issue', hint: 'C', run: onCreate })
    actions.push({ value: 'my-issues', label: 'Go to My issues', run: () => go('my-issues') })
    actions.push({ value: 'projects', label: 'Go to Projects', run: () => go('projects') })
    actions.push({ value: 'roadmap', label: 'Go to Roadmap', run: () => go('roadmap') })
    const current = cycles?.key === projectKey ? cycles?.list.find((c) => c.state === 'current') : undefined
    if (current) {
      actions.push({
        value: 'current-cycle',
        label: 'Go to current cycle',
        run: () => go(`projects/${encodeURIComponent(cycles!.key)}/cycles/${current.number}`),
      })
    }
    actions.push({ value: 'inbox', label: 'Go to Inbox', run: () => go('inbox') })
    for (const c of chats) actions.push({ value: `chat-${c.id}`, label: `Go to ${c.name}`, run: () => go(`inbox/c/${encodeURIComponent(c.id)}`) })
    actions.push({ value: 'feedback', label: 'Go to Feedback', run: () => go('feedback') })
    actions.push({ value: 'add-feedback', label: 'Add feedback', run: () => go('feedback?add=true') })
    actions.push({ value: 'new-message', label: 'New message', run: () => go('inbox?new=message') })
    actions.push({ value: 'new-group', label: 'New group', run: () => go('inbox?new=group') })
    for (const name of can(workspace, 'integrations.manage') ? [...SETTINGS, 'Integrations'] : SETTINGS) actions.push({ value: name, label: `Go to settings → ${name}`, run: () => go(`settings/${name.toLowerCase().replaceAll(' ', '-')}`) })
    if (can(workspace, 'feedback.manage')) actions.push({ value: 'Feedback form', label: 'Go to settings → Feedback form', run: () => go('settings/feedback') })
    if (ctx) {
      if (ctx.issue.can_edit) {
        for (const m of ['status', 'assignee', 'priority', 'labels'] as const) {
          actions.push({ value: m, label: MODE_ACTION[m][0], hint: MODE_ACTION[m][1], run: () => enter(m) })
        }
      }
      const watching = ctx.issue.watching
      actions.push({
        value: 'watch',
        label: watching ? 'Unwatch issue' : 'Watch issue',
        run: () =>
          api(`${workspacePath(slug)}/issues/${encodeURIComponent(ctx.issue.identifier)}/watch`, undefined, watching ? 'DELETE' : 'POST').then(
            () => {
              onClose()
              reloadPage(router)
            },
            (e) => setError(toFormErrors(e).error),
          ),
      })
      const copy = (text: string) => () =>
        navigator.clipboard.writeText(text).then(onClose, () => setError("Couldn't copy. Try again."))
      actions.push({ value: 'copy-link', label: 'Copy issue link', run: copy(`${location.origin}/${slug}/issue/${ctx.issue.identifier}`) })
      actions.push({ value: 'copy-id', label: 'Copy issue ID', run: copy(ctx.issue.identifier) })
      if (copyBranch) {
        actions.push({
          value: 'copy-branch',
          label: 'Copy branch name',
          hint: copyBranch.key,
          run: () => {
            onClose()
            copyBranch.run()
          },
        })
      }
    }
    const matched = filterOptions(actions, q) as typeof actions
    if (matched.length) {
      groups.push({
        label: 'Actions',
        items: matched.map((a) => ({ id: `action-${a.value}`, label: a.label, hint: a.hint ? <kbd className="kbd">{a.hint}</kbd> : undefined, run: a.run })),
      })
    }
  } else if (ctx) {
    const { issue } = ctx
    if (mode === 'status') {
      const opts = filterOptions(statusOptions(ctx.statuses), q)
      groups.push({
        label: 'Status',
        items: opts.map((o) => {
          const status = ctx.statuses.find((s) => s.id === o.value)!
          return { id: o.value, label: o.label, lead: <StatusIcon status={status} />, checked: o.value === issue.status.id, run: () => save('status_id', [o.value]) }
        }),
      })
    } else if (mode === 'assignee') {
      const opts = filterOptions(assigneeOptions(people?.list ?? []), q)
      groups.push({
        label: 'Assignee',
        items: opts.map((o) => ({ id: o.value || 'none', label: o.label, hint: o.hint, checked: o.value === (issue.assignee?.id ?? ''), run: () => save('assignee_id', [o.value]) })),
      })
    } else if (mode === 'priority') {
      const opts = filterOptions(priorityOptions, q)
      groups.push({
        label: 'Priority',
        items: opts.map((o) => ({ id: o.value, label: o.label, checked: o.value === String(issue.priority), run: () => save('priority', [o.value]) })),
      })
    } else {
      const opts = filterOptions(ctx.labels.map((l) => ({ value: l.id, label: l.name })), q)
      groups.push({
        label: 'Labels',
        items: opts.map((o) => {
          const checked = labelIds.includes(o.value)
          return {
            id: o.value,
            label: o.label,
            lead: <LabelDot color={ctx.labels.find((l) => l.id === o.value)!.color} />,
            checked,
            run: () => {
              const prev = labelIds
              const next = checked ? prev.filter((x) => x !== o.value) : [...prev, o.value]
              setLabelIds(next)
              save('label_ids', next, true).then((ok) => !ok && setLabelIds(prev))
            },
          }
        }),
      })
    }
  }

  const items = groups.flatMap((g) => g.items)
  const current = Math.min(active, items.length - 1)
  const stale = mode === 'root' && found?.q !== q

  useEffect(() => {
    listRef.current?.querySelector('[data-active]')?.scrollIntoView({ block: 'nearest' })
  }, [current])

  useEffect(() => {
    if (!queued || stale) return
    setQueued(false)
    items[current]?.run()
  })

  function onKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      if (items.length) setActive((current + (e.key === 'ArrowDown' ? 1 : -1) + items.length) % items.length)
    } else if (e.key === 'Enter') {
      e.preventDefault()
      if (stale && q) setQueued(true)
      else items[current]?.run()
    } else if (e.key === 'Backspace' && query === '' && mode !== 'root') {
      e.preventDefault()
      enter('root')
    }
  }

  const multi = mode === 'labels'
  let index = 0
  return (
    <div className="grid">
      <div className="palette-input-row">
        {mode !== 'root' && ctx ? (
          <span className="palette-chip">
            {ctx.issue.identifier} › {MODE_LABEL[mode]}
          </span>
        ) : null}
        <input
          className="palette-input"
          role="combobox"
          aria-label={mode === 'root' ? 'Search or type a command' : MODE_ACTION[mode][0]}
          aria-expanded
          aria-controls={`${id}-list`}
          aria-autocomplete="list"
          aria-activedescendant={current >= 0 ? `${id}-${current}` : undefined}
          aria-describedby={error ? `${id}-error` : undefined}
          autoComplete="off"
          autoCapitalize="off"
          spellCheck={false}
          placeholder={mode === 'root' ? 'Search or type a command…' : MODE_ACTION[mode][0]}
          value={query}
          onChange={(e) => {
            setQuery(e.target.value)
            setActive(0)
          }}
          onKeyDown={onKeyDown}
        />
      </div>
      {error ? (
        <p id={`${id}-error`} role="alert" className="px-4 pt-2 m-0 text-caption text-accent">
          {error}
        </p>
      ) : null}
      <div ref={listRef} id={`${id}-list`} role="listbox" aria-label="Results" aria-multiselectable={multi || undefined} className="palette-list">
        {items.length === 0 ? (
          <p className="palette-note" role="status">
            {mode === 'root' && !found ? 'Searching…' : 'No results'}
          </p>
        ) : (
          groups.map((g) => (
            <div key={g.label} role="group" aria-labelledby={`${id}-${g.label}`}>
              <div id={`${id}-${g.label}`} role="presentation" className="palette-group">
                {g.label}
              </div>
              {g.items.map((item) => {
                const i = index++
                return (
                  <div
                    key={item.id}
                    id={`${id}-${i}`}
                    role="option"
                    aria-selected={mode === 'root' ? i === current : Boolean(item.checked)}
                    data-active={i === current || undefined}
                    className="palette-option"
                    onPointerDown={(e) => e.preventDefault()}
                    onPointerMove={() => i !== current && setActive(i)}
                    onClick={() => {
                      setActive(i)
                      item.run()
                    }}
                  >
                    {item.lead}
                    <span className="ellipsis flex-grow" style={{ color: 'var(--text)' }}>
                      {item.label}
                    </span>
                    {item.hint ? <span className="palette-hint">{item.hint}</span> : null}
                    {item.checked ? <CheckIcon size={16} strokeWidth={2} className="flex-noshrink" /> : null}
                  </div>
                )
              })}
            </div>
          ))
        )}
      </div>
    </div>
  )
}

