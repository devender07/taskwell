import type { ComponentProps } from 'react'
import { useTooltip } from '../tooltip/tooltip'
import './icon-button.css'

// 40px square button for a single 20px icon. aria-label is required since there is no visible text. `tooltip` shows a
// label on hover and keyboard focus; leave it off when the icon says it all (a close ×).
type IconButtonProps = ComponentProps<'button'> & { 'aria-label': string; tooltip?: string }

export function IconButton({ className, type = 'button', tooltip: text, onPointerEnter, onPointerLeave, onPointerDown, onFocus, onBlur, ...props }: IconButtonProps) {
  const { handlers: h, tooltip } = useTooltip(text)
  return (
    <>
      <button
        type={type}
        className={className ? `icon-button ${className}` : 'icon-button'}
        onPointerEnter={(e) => (onPointerEnter?.(e), h?.onPointerEnter(e))}
        onPointerLeave={(e) => (onPointerLeave?.(e), h?.onPointerLeave())}
        onPointerDown={(e) => (onPointerDown?.(e), h?.onPointerDown())}
        onFocus={(e) => (onFocus?.(e), h?.onFocus(e))}
        onBlur={(e) => (onBlur?.(e), h?.onBlur())}
        {...props}
      />
      {tooltip}
    </>
  )
}
