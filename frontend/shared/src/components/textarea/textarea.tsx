import { useId, type ComponentProps } from 'react'
import { useFieldError } from '../input/field-error'
import '../input/input.css'
import './textarea.css'

type TextareaProps = ComponentProps<'textarea'> & { label?: string; error?: string | null }

export function Textarea({ className, label, error: rawError, id, onChange, ...props }: TextareaProps) {
  const [error, onEdit] = useFieldError(rawError, props.value)
  const autoId = useId()
  const inputId = id ?? autoId
  const errorId = `${inputId}-error`

  return (
    <div className="grid gap-2">
      {label ? <label htmlFor={inputId} className="text-label text-secondary">{label}</label> : null}
      <textarea
        id={inputId}
        className={className ? `input textarea ${className}` : 'input textarea'}
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? errorId : undefined}
        onChange={(e) => {
          onEdit()
          onChange?.(e)
        }}
        {...props}
      />
      {error ? <span id={errorId} className="text-caption text-accent">{error}</span> : null}
    </div>
  )
}
