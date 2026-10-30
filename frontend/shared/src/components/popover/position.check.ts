// Run: node --experimental-strip-types shared/src/components/popover/position.check.ts
import assert from 'node:assert/strict'
import { computePosition } from './position.ts'

const vp = { width: 1000, height: 800 }
const panel = { width: 200, height: 100 }
const at = (top: number, left: number) => ({ top, left, width: 40, height: 40 })

// Requested side with room: used as-is, aligned to the anchor's end.
assert.deepEqual(computePosition(at(10, 900), panel, 'bottom-end', vp), { top: 58, left: 740, side: 'bottom', arrow: 180 })
// No room below, room above: flips to top.
assert.deepEqual(computePosition(at(740, 100), panel, 'bottom-start', vp), { top: 632, left: 100, side: 'top', arrow: 20 })
// No room above, room below: flips to bottom.
assert.equal(computePosition(at(20, 100), panel, 'top', vp).side, 'bottom')
// Start-aligned near the right edge: clamped inside the viewport.
assert.equal(computePosition(at(10, 950), panel, 'bottom-start', vp).left, 792)
// Right side without room flips left; left side clamps at the padding.
assert.deepEqual(computePosition(at(300, 900), panel, 'right', vp), { top: 270, left: 692, side: 'left', arrow: 50 })
// Neither side fits: keeps the requested side and clamps.
assert.equal(computePosition(at(380, 100), { width: 200, height: 790 }, 'bottom', vp).top, 8)
// Clamped panel: the arrow still points at the anchor's center (970), not the panel's middle.
assert.equal(computePosition(at(10, 950), panel, 'bottom', vp).arrow, 970 - 792)
// Anchor past the panel's corner: arrow stops at the inset.
assert.equal(computePosition({ top: 10, left: 995, width: 4, height: 4 }, panel, 'bottom-end', vp).arrow, 188)
console.log('position: ok')
