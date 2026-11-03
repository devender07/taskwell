import { useEffect, useId, useRef, useState, type KeyboardEvent } from 'react'
import { useFieldError } from '../input/field-error'
import { CalendarIcon, ChevronLeftIcon, ChevronRightIcon } from '../icons/icons'
import { Popover } from '../popover/popover'
import { addDays, addMonths, monthGrid, parseISO, sameMonth, toISO } from './calendar'
import '../input/input.css'
import './date-picker.css'

type DatePickerProps = {
  label?: string
  'aria-label'?: string
  value: string
  onChange: (value: string) => void
  name?: string
  error?: string | null
  placeholder?: string
  disabled?: boolean
}

const WEEKDAYS = ['Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa', 'Su']

// Field showing a 'YYYY-MM-DD' value ('' = none) that opens a month calendar. `name` submits it with a form.
export function DatePicker({ label, 'aria-label': ariaLabel, value, onChange, name, error: rawError, placeholder = 'Pick a date', disabled }: DatePickerProps) {
  const id = useId()
  const errorId = `${id}-error`
  const [error, onEdit] = useFieldError(rawError, value)
  const date = parseISO(value)

  return (
    <div className="grid gap-2">
      {label ? <label htmlFor={id} className="text-label text-secondary">{label}</label> : null}
      <Popover
        aria-label={label ?? ariaLabel ?? 'Calendar'}
        arrow={false}
        trigger={(props) => (
          <button
            {...props}
            id={id}
            type="button"
            className="input date-picker-trigger"
            aria-label={label ? undefined : ariaLabel}
            aria-haspopup="dialog"
            aria-invalid={error ? true : undefined}
            aria-describedby={error ? errorId : undefined}
            disabled={disabled}
          >
            <span className={date ? 'ellipsis' : 'ellipsis text-secondary'}>
              {date ? date.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' }) : placeholder}
            </span>
            <CalendarIcon size={16} className="date-picker-icon" />
          </button>
        )}
      >
        {(close) => (
          <Calendar
            value={value}
            onPick={(v) => {
              onEdit()
              onChange(v)
              close()
            }}
          />
        )}
      </Popover>
      {name ? <input type="hidden" name={name} value={value} /> : null}
      {error ? <span id={errorId} className="text-caption text-accent">{error}</span> : null}
    </div>
  )
}

function Calendar({ value, onPick }: { value: string; onPick: (value: string) => void }) {
  const today = toISO(new Date())
  const [focused, setFocused] = useState(parseISO(value) ? value : today)
  const gridRef = useRef<HTMLDivElement>(null)
  const moved = useRef(false)
  const days = monthGrid(focused)
  const title = parseISO(focused)!.toLocaleDateString(undefined, { month: 'long', year: 'numeric' })

  useEffect(() => {
    gridRef.current?.querySelector<HTMLButtonElement>(`[data-date="${focused}"]`)?.focus({ preventScroll: !moved.current })
  }, [focused])

  function onKeyDown(e: KeyboardEvent) {
    const step: Record<string, () => string> = {
      ArrowLeft: () => addDays(focused, -1),
      ArrowRight: () => addDays(focused, 1),
      ArrowUp: () => addDays(focused, -7),
      ArrowDown: () => addDays(focused, 7),
      PageUp: () => addMonths(focused, -1),
      PageDown: () => addMonths(focused, 1),
      Home: () => addDays(focused, -((parseISO(focused)!.getDay() + 6) % 7)),
      End: () => addDays(focused, 6 - ((parseISO(focused)!.getDay() + 6) % 7)),
    }
    const next = step[e.key]
    if (!next) return
    e.preventDefault()
    moved.current = true
    setFocused(next())
  }

  return (
    <div className="date-picker">
      <div className="date-picker-header">
        <button type="button" className="date-picker-nav" aria-label="Previous month" onClick={() => setFocused(addMonths(focused, -1))}>
          <ChevronLeftIcon size={16} />
        </button>
        <span className="text-label" aria-live="polite">{title}</span>
        <button type="button" className="date-picker-nav" aria-label="Next month" onClick={() => setFocused(addMonths(focused, 1))}>
          <ChevronRightIcon size={16} />
        </button>
      </div>
      <div ref={gridRef} role="grid" aria-label={title} className="date-picker-grid" onKeyDown={onKeyDown}>
        {WEEKDAYS.map((d) => (
          <span key={d} role="columnheader" className="date-picker-weekday">{d}</span>
        ))}
        {days.map((d) => (
          <button
            key={d}
            type="button"
            role="gridcell"
            data-date={d}
            tabIndex={d === focused ? 0 : -1}
            aria-selected={d === value}
            aria-current={d === today ? 'date' : undefined}
            data-outside={sameMonth(d, focused) ? undefined : ''}
            className="date-picker-day"
            aria-label={parseISO(d)!.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' })}
            onClick={() => onPick(d)}
          >
            {Number(d.slice(8))}
          </button>
        ))}
      </div>
      <div className="date-picker-footer">
        <button type="button" className="date-picker-link" onClick={() => onPick(today)}>Today</button>
        {value ? <button type="button" className="date-picker-link" onClick={() => onPick('')}>Clear</button> : null}
      </div>
    </div>
  )
}
