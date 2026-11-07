import { useEffect, useId, useRef, useState, type ComponentProps } from 'react'
import { EyeIcon, EyeOffIcon } from '../icons/icons'
import { useFieldError } from './field-error'
import './input.css'

// prefix: fixed text shown inside the field before the value, e.g. "taskwell.dev/".
// A password field gets a show/hide toggle at its end.
// onDebouncedChange: called with the value once typing pauses for `debounce` ms (default 300), e.g. for a search.
type InputProps = Omit<ComponentProps<'input'>, 'prefix'> & {
  label?: string
  error?: string | null
  prefix?: string
  debounce?: number
  onDebouncedChange?: (value: string) => void
}

export function Input({ className, type = 'text', label, error: rawError, prefix, id, onChange, debounce = 300, onDebouncedChange, ...props }: InputProps) {
  const [error, onEdit] = useFieldError(rawError, props.value)
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined)
  useEffect(() => () => clearTimeout(timer.current), [])
  const autoId = useId()
  const inputId = id ?? autoId
  const errorId = `${inputId}-error`
  const [shown, setShown] = useState(false)
  const password = type === 'password'

  const input = (
    <input
      id={inputId}
      type={password && shown ? 'text' : type}
      className={prefix ? className : `input${password ? ' input-password' : ''}${className ? ` ${className}` : ''}`}
      aria-invalid={error ? true : undefined}
      aria-describedby={error ? errorId : undefined}
      onChange={(e) => {
        onEdit()
        onChange?.(e)
        if (onDebouncedChange) {
          const value = e.target.value
          clearTimeout(timer.current)
          timer.current = setTimeout(() => onDebouncedChange(value), debounce)
        }
      }}
      {...props}
    />
  )

  return (
    <div className="grid gap-2">
      {label ? <label htmlFor={inputId} className="text-label text-secondary">{label}</label> : null}
      {prefix ? (
        <div className="input input-prefixed">
          <span aria-hidden>{prefix}</span>
          {input}
        </div>
      ) : password ? (
        <div className="relative">
          {input}
          <button
            type="button"
            className="input-reveal"
            aria-label={shown ? 'Hide password' : 'Show password'}
            aria-controls={inputId}
            aria-pressed={shown}
            disabled={props.disabled}
            onClick={() => setShown(!shown)}
          >
            {shown ? <EyeOffIcon /> : <EyeIcon />}
          </button>
        </div>
      ) : (
        input
      )}
      {error ? <span id={errorId} className="text-caption text-accent">{error}</span> : null}
    </div>
  )
}
