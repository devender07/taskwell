import { useEffect, useId, useLayoutEffect, useRef, useState, type KeyboardEvent } from 'react'
import { useFieldError } from '../input/field-error'
import { filterOptions, splitMatch, type Option } from './match'
import '../input/input.css'
import './smart-autocomplete.css'

export type { Option }

type SmartAutocompleteProps = {
  label?: string
  'aria-label'?: string
  error?: string | null
  name?: string
  placeholder?: string
  options?: Option[]
  loadOptions?: (query: string, signal: AbortSignal) => Promise<Option[]>
  // With loadOptions: registers `reload` while the list is open (e.g. for live updates); returns the unsubscribe.
  subscribe?: (reload: () => void) => () => void
  value?: Option | null
  defaultValue?: Option | null
  onChange?: (option: Option | null) => void
  emptyText?: string
  disabled?: boolean
  // Lets the user add what they typed when nothing matches it exactly: an extra option with value `new:<text>`,
  // the text as its label and this as its hint (e.g. "New type").
  create?: string
}

const DEBOUNCE_MS = 200

// Input with a filtered dropdown (ARIA combobox). Pass `options` to filter in the browser, or `loadOptions` to
// fetch as you type (debounced, stale requests aborted). `name` submits the selected value with a form.
export function SmartAutocomplete({
  label,
  'aria-label': ariaLabel,
  error: rawError,
  name,
  placeholder,
  options = [],
  loadOptions,
  subscribe,
  value,
  defaultValue = null,
  onChange,
  emptyText = 'No results',
  disabled,
  create,
}: SmartAutocompleteProps) {
  const id = useId()
  const listId = `${id}-list`
  const errorId = `${id}-error`
  const [error, onEdit] = useFieldError(rawError)
  const [own, setOwn] = useState(defaultValue)
  const selected = value !== undefined ? value : own
  const [query, setQuery] = useState(selected?.label ?? '')
  const [prevSelected, setPrevSelected] = useState(selected)
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(0)
  const [loaded, setLoaded] = useState<Option[]>([])
  const [loading, setLoading] = useState(false)
  const listRef = useRef<HTMLUListElement>(null)
  const rootRef = useRef<HTMLDivElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  const pressing = useRef(false)
  const loadRef = useRef(loadOptions)
  const subscribeRef = useRef(subscribe)
  const [version, setVersion] = useState(0)

  // Compared by value: a refetch that rebuilds the same option keeps what's being typed.
  if (selected !== prevSelected) {
    setPrevSelected(selected)
    if (selected?.value !== prevSelected?.value || selected?.label !== prevSelected?.label) setQuery(selected?.label ?? '')
  }

  // Showing the selected label means "not searching yet": list everything.
  const term = query === (selected?.label ?? '') ? '' : query
  const found = loadOptions ? loaded : filterOptions(options, term)
  const typed = term.trim()
  const results =
    create && typed && !found.some((o) => o.label.toLowerCase() === typed.toLowerCase())
      ? [...found, { value: `new:${typed}`, label: typed, hint: create }]
      : found
  const clamped = Math.min(active, results.length - 1)
  const current = results[clamped]?.disabled ? step(results, clamped, 1) : clamped

  useEffect(() => {
    loadRef.current = loadOptions
    subscribeRef.current = subscribe
  })

  const remote = Boolean(loadOptions)
  useEffect(() => {
    if (remote && open) return subscribeRef.current?.(() => setVersion((v) => v + 1))
  }, [remote, open])

  useEffect(() => {
    if (!remote || !open) return
    const ctrl = new AbortController()
    setLoading(true)
    const t = setTimeout(() => {
      loadRef.current?.(term, ctrl.signal).then(
        (opts) => {
          setLoaded(opts)
          setActive(0)
          setLoading(false)
        },
        () => {
          if (ctrl.signal.aborted) return
          setLoaded([])
          setLoading(false)
        },
      )
    }, DEBOUNCE_MS)
    return () => {
      clearTimeout(t)
      ctrl.abort()
    }
  }, [remote, open, term, version])

  // iOS blurs the input when an option is tapped, before the click lands: a press in the list keeps it open
  // through that blur, and a press outside closes it.
  useEffect(() => {
    if (!open) return
    const down = (e: PointerEvent) => {
      const target = e.target as Node
      pressing.current = !!panelRef.current?.contains(target)
      if (!rootRef.current?.contains(target)) close()
    }
    document.addEventListener('pointerdown', down, true)
    return () => document.removeEventListener('pointerdown', down, true)
  })

  useEffect(() => {
    if (open && current >= 0) listRef.current?.children[current]?.scrollIntoView({ block: 'nearest' })
  }, [open, current])

  // The list is position: fixed under the field (above it when there's more room there), so a dialog's or card's
  // overflow can't clip it. It follows scrolling and resizing while open.
  useLayoutEffect(() => {
    const panel = panelRef.current
    const anchor = panel?.parentElement
    if (!open || !panel || !anchor) return
    const place = () => {
      const r = anchor.getBoundingClientRect()
      const below = innerHeight - r.bottom - 8
      const up = below < Math.min(panel.offsetHeight, 240) && r.top - 8 > below
      panel.style.left = `${r.left}px`
      panel.style.width = `${r.width}px`
      panel.style.top = up ? '' : `${r.bottom + 4}px`
      panel.style.bottom = up ? `${innerHeight - r.top + 4}px` : ''
    }
    place()
    addEventListener('scroll', place, true)
    addEventListener('resize', place)
    return () => {
      removeEventListener('scroll', place, true)
      removeEventListener('resize', place)
    }
  })

  // Opening highlights the selected option, so arrows move from where you are.
  function openList() {
    if (open) return
    setOpen(true)
    setActive(Math.max(0, results.findIndex((o) => o.value === selected?.value)))
  }

  function close() {
    pressing.current = false
    setOpen(false)
    setQuery(selected?.label ?? '')
  }

  function choose(option: Option | null) {
    pressing.current = false
    onEdit()
    setOwn(option)
    onChange?.(option)
    setQuery(option?.label ?? '')
    setOpen(false)
  }

  function onKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    pressing.current = false
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      if (!open) return openList()
      setActive(step(results, current, e.key === 'ArrowDown' ? 1 : -1))
    } else if (e.key === 'Enter' && open && results[current] && !results[current].disabled) {
      e.preventDefault()
      choose(results[current])
    } else if (e.key === 'Escape' && open) {
      e.preventDefault()
      close()
    }
  }

  return (
    <div className="grid gap-2">
      {label ? <label htmlFor={id} className="text-label text-secondary">{label}</label> : null}
      <div ref={rootRef} className="autocomplete">
        <input
          id={id}
          className="input autocomplete-input"
          role="combobox"
          aria-label={ariaLabel}
          aria-expanded={open}
          aria-controls={listId}
          aria-autocomplete="list"
          aria-activedescendant={open && current >= 0 ? `${id}-${current}` : undefined}
          aria-invalid={error ? true : undefined}
          aria-describedby={error ? errorId : undefined}
          autoComplete="off"
          spellCheck={false}
          placeholder={placeholder}
          disabled={disabled}
          value={query}
          onChange={(e) => {
            onEdit()
            setQuery(e.target.value)
            setActive(0)
            setOpen(true)
            if (!e.target.value && selected) {
              setOwn(null)
              onChange?.(null)
            }
          }}
          onClick={openList}
          onKeyDown={onKeyDown}
          onBlur={() => !pressing.current && close()}
        />
        {name ? <input type="hidden" name={name} value={selected?.value ?? ''} /> : null}
        {open ? (
          <div ref={panelRef} className="autocomplete-panel">
            {loading || results.length === 0 ? (
              <p className="autocomplete-note" role="status">{loading ? 'Searching…' : emptyText}</p>
            ) : (
              <ul id={listId} ref={listRef} role="listbox" aria-label={label ?? ariaLabel} className="autocomplete-list">
                {results.map((o, i) => (
                  <li
                    key={o.value}
                    id={`${id}-${i}`}
                    role="option"
                    aria-selected={o.value === selected?.value}
                    aria-disabled={o.disabled || undefined}
                    data-active={i === current || undefined}
                    className="autocomplete-option"
                    onPointerDown={(e) => e.preventDefault()}
                    onPointerMove={() => setActive(i)}
                    onClick={() => !o.disabled && choose(o)}
                  >
                    <span className="flex items-center gap-2" style={{ minWidth: 0 }}>
                      {o.icon}
                      <span className="ellipsis"><Highlight text={o.label} query={term} /></span>
                    </span>
                    {o.hint ? <span className="autocomplete-hint ellipsis">{o.hint}</span> : null}
                  </li>
                ))}
              </ul>
            )}
          </div>
        ) : null}
      </div>
      {error ? <span id={errorId} className="text-caption text-accent">{error}</span> : null}
    </div>
  )
}

// Index of the next enabled option from `from` in direction `dir`, wrapping; `from` when there is none.
function step(options: Option[], from: number, dir: 1 | -1) {
  const n = options.length
  for (let k = 1; k <= n; k++) {
    const i = (((from + dir * k) % n) + n) % n
    if (!options[i]?.disabled) return i
  }
  return from
}

function Highlight({ text, query }: { text: string; query: string }) {
  const m = splitMatch(text, query)
  if (!m) return text
  return (
    <>
      {m[0]}
      <mark>{m[1]}</mark>
      {m[2]}
    </>
  )
}
