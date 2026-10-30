import { useId, type ComponentProps } from 'react'
import { useFieldError } from './field-error'
import './input.css'

// prefix: fixed text shown inside the field before the value, e.g. "tether.app/".
type InputProps = Omit<ComponentProps<'input'>, 'prefix'> & { label?: string; error?: string | null; prefix?: string }

export function Input({ className, type = 'text', label, error: rawError, prefix, id, onChange, ...props }: InputProps) {
  const [error, onEdit] = useFieldError(rawError, props.value)
  const autoId = useId()
  const inputId = id ?? autoId
  const errorId = `${inputId}-error`

  const input = (
    <input
      id={inputId}
      type={type}
      className={prefix ? className : className ? `input ${className}` : 'input'}
      aria-invalid={error ? true : undefined}
      aria-describedby={error ? errorId : undefined}
      onChange={(e) => {
        onEdit()
        onChange?.(e)
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
      ) : (
        input
      )}
      {error ? <span id={errorId} className="text-caption text-accent">{error}</span> : null}
    </div>
  )
}
