import type { ComponentProps } from 'react'
import './icon-button.css'

// 40px square button for a single 20px icon. aria-label is required since there is no visible text.
type IconButtonProps = ComponentProps<'button'> & { 'aria-label': string }

export function IconButton({ className, type = 'button', ...props }: IconButtonProps) {
  return <button type={type} className={className ? `icon-button ${className}` : 'icon-button'} {...props} />
}
