import type { ComponentProps } from 'react'
import './button.css'

type ButtonProps = ComponentProps<'button'> & { variant?: 'primary' | 'secondary' | 'ghost' | 'danger' }

export function Button({ className, variant = 'primary', type = 'button', ...props }: ButtonProps) {
  const cls = `button button-${variant}`
  return <button type={type} className={className ? `${cls} ${className}` : cls} {...props} />
}
