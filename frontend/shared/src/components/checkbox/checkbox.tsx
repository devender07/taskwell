import type { ComponentProps, ReactNode } from 'react'
import './checkbox.css'

type CheckboxProps = Omit<ComponentProps<'input'>, 'type'> & { label: ReactNode }

export function Checkbox({ label, className, ...props }: CheckboxProps) {
  return (
    <label className={className ? `checkbox ${className}` : 'checkbox'}>
      <input type="checkbox" {...props} />
      {label}
    </label>
  )
}
