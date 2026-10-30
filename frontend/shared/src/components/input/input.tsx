import { useId, type ComponentProps } from 'react'
import './input.css'

type InputProps = ComponentProps<'input'> & { label?: string; error?: string | null }

export function Input({ className, type = 'text', label, error, id, ...props }: InputProps) {
  const autoId = useId()
  const inputId = id ?? autoId
  const errorId = `${inputId}-error`

  return (
    <div className="grid gap-2">
      {label ? <label htmlFor={inputId} className="text-label text-secondary">{label}</label> : null}
      <input
        id={inputId}
        type={type}
        className={className ? `input ${className}` : 'input'}
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? errorId : undefined}
        {...props}
      />
      {error ? <span id={errorId} className="text-caption text-accent">{error}</span> : null}
    </div>
  )
}
