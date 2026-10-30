export type Side = 'top' | 'bottom' | 'left' | 'right'
export type Placement = Side | `${Side}-start` | `${Side}-end`

type Rect = { top: number; left: number; width: number; height: number }
type Size = { width: number; height: number }

const opposite: Record<Side, Side> = { top: 'bottom', bottom: 'top', left: 'right', right: 'left' }

// Places a panel of `panel` size next to `anchor`. Flips to the opposite side when the requested side lacks room
// and the opposite has it, then clamps the panel inside the viewport (minus `padding`). `arrow` is the distance
// along the panel's edge to the anchor's center, kept `arrowInset` from the panel's corners.
export function computePosition(
  anchor: Rect,
  panel: Size,
  placement: Placement,
  viewport: Size,
  offset = 8,
  padding = 8,
  arrowInset = 12,
): { top: number; left: number; side: Side; arrow: number } {
  const [requested, align = 'center'] = placement.split('-') as [Side, 'start' | 'end' | undefined]
  const right = anchor.left + anchor.width
  const bottom = anchor.top + anchor.height
  const room: Record<Side, number> = {
    top: anchor.top - offset - padding,
    bottom: viewport.height - bottom - offset - padding,
    left: anchor.left - offset - padding,
    right: viewport.width - right - offset - padding,
  }
  const need = (s: Side) => (s === 'top' || s === 'bottom' ? panel.height : panel.width)
  const side = room[requested] < need(requested) && room[opposite[requested]] >= need(opposite[requested]) ? opposite[requested] : requested

  let top: number
  let left: number
  if (side === 'top' || side === 'bottom') {
    top = side === 'bottom' ? bottom + offset : anchor.top - offset - panel.height
    left = align === 'start' ? anchor.left : align === 'end' ? right - panel.width : anchor.left + (anchor.width - panel.width) / 2
  } else {
    left = side === 'right' ? right + offset : anchor.left - offset - panel.width
    top = align === 'start' ? anchor.top : align === 'end' ? bottom - panel.height : anchor.top + (anchor.height - panel.height) / 2
  }

  const clamp = (v: number, size: number, max: number) => Math.max(padding, Math.min(v, max - padding - size))
  top = clamp(top, panel.height, viewport.height)
  left = clamp(left, panel.width, viewport.width)
  const vertical = side === 'top' || side === 'bottom'
  const center = vertical ? anchor.left + anchor.width / 2 - left : anchor.top + anchor.height / 2 - top
  const edge = vertical ? panel.width : panel.height
  return { top, left, side, arrow: Math.max(arrowInset, Math.min(center, edge - arrowInset)) }
}
