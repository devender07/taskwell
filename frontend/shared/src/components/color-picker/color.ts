// Hex colors with optional alpha: '#rrggbb' (opaque) or '#rrggbbaa'. h 0-360, s/v/a 0-1.
export type HSVA = { h: number; s: number; v: number; a: number }

export const HEX = /^#([0-9a-f]{6}|[0-9a-f]{8})$/

export function parseHex(hex: string): HSVA | null {
  const v = hex.trim().toLowerCase()
  if (!HEX.test(v)) return null
  const n = (i: number) => parseInt(v.slice(i, i + 2), 16) / 255
  const [r, g, b] = [n(1), n(3), n(5)]
  const max = Math.max(r, g, b)
  const d = max - Math.min(r, g, b)
  let h = 0
  if (d) h = max === r ? ((g - b) / d) % 6 : max === g ? (b - r) / d + 2 : (r - g) / d + 4
  return { h: (h * 60 + 360) % 360, s: max ? d / max : 0, v: max, a: v.length === 9 ? n(7) : 1 }
}

export function toHex({ h, s, v, a }: HSVA) {
  const f = (k: number) => {
    const x = (k + h / 60) % 6
    return v - v * s * Math.max(0, Math.min(x, 4 - x, 1))
  }
  const byte = (x: number) => Math.round(x * 255).toString(16).padStart(2, '0')
  return `#${byte(f(5))}${byte(f(3))}${byte(f(1))}${a < 1 ? byte(a) : ''}`
}
