import { useEffect, useId, useRef, useState, type KeyboardEvent } from 'react'
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
  value?: Option | null
  defaultValue?: Option | null
  onChange?: (option: Option | null) => void
  emptyText?: string
  disabled?: boolean
}

const DEBOUNCE_MS = 200

// Input with a filtered dropdown (ARIA combobox). Pass `options` to filter in the browser, or `loadOptions` to
// fetch as you type (debounced, stale requests aborted). `name` submits the selected value with a form.
// ponytail: the list is absolutely positioned, so an `overflow: hidden` ancestor clips it; portal it like Popover if that bites.
export function SmartAutocomplete({
  label,
  'aria-label': ariaLabel,
  error,
  name,
  placeholder,
  options = [],
  loadOptions,
  value,
  defaultValue = null,
  onChange,
  emptyText = 'No results',
  disabled,
}: SmartAutocompleteProps) {
  const id = useId()
  const listId = `${id}-list`
  const errorId = `${id}-error`
  const [own, setOwn] = useState(defaultValue)
  const selected = value !== undefined ? value : own
  const [query, setQuery] = useState(selected?.label ?? '')
  const [prevSelected, setPrevSelected] = useState(selected)
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(0)
  const [loaded, setLoaded] = useState<Option[]>([])
  const [loading, setLoading] = useState(false)
  const listRef = useRef<HTMLUListElement>(null)
  const loadRef = useRef(loadOptions)

  if (selected !== prevSelected) {
    setPrevSelected(selected)
    setQuery(selected?.label ?? '')
  }

  // Showing the selected label means "not searching yet": list everything.
  const term = query === (selected?.label ?? '') ? '' : query
  const results = loadOptions ? loaded : filterOptions(options, term)
  const clamped = Math.min(active, results.length - 1)
  const current = results[clamped]?.disabled ? step(results, clamped, 1) : clamped

  useEffect(() => {
    loadRef.current = loadOptions
  })

  const remote = Boolean(loadOptions)
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
  }, [remote, open, term])

  useEffect(() => {
    if (open && current >= 0) listRef.current?.children[current]?.scrollIntoView({ block: 'nearest' })
  }, [open, current])

  // Opening highlights the selected option, so arrows move from where you are.
  function openList() {
    if (open) return
    setOpen(true)
    setActive(Math.max(0, results.findIndex((o) => o.value === selected?.value)))
  }

  function choose(option: Option | null) {
    setOwn(option)
    onChange?.(option)
    setQuery(option?.label ?? '')
    setOpen(false)
  }

  function onKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      if (!open) return openList()
      setActive(step(results, current, e.key === 'ArrowDown' ? 1 : -1))
    } else if (e.key === 'Enter' && open && results[current] && !results[current].disabled) {
      e.preventDefault()
      choose(results[current])
    } else if (e.key === 'Escape' && open) {
      e.preventDefault()
      setOpen(false)
      setQuery(selected?.label ?? '')
    }
  }

  return (
    <div className="grid gap-2">
      {label ? <label htmlFor={id} className="text-label text-secondary">{label}</label> : null}
      <div className="autocomplete">
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
          onBlur={() => {
            setOpen(false)
            setQuery(selected?.label ?? '')
          }}
        />
        {name ? <input type="hidden" name={name} value={selected?.value ?? ''} /> : null}
        {open ? (
          <div className="autocomplete-panel">
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
                    <span className="ellipsis"><Highlight text={o.label} query={term} /></span>
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
