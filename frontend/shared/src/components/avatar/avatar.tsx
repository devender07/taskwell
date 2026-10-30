// Initials in a circle; swap for an <img> when profile photos exist.
export function Avatar({ name, size = 32 }: { name: string; size?: number }) {
  const initials = name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((word) => word[0]!.toUpperCase())
    .join('')

  return (
    <span
      aria-hidden
      className="inline-grid place-center rounded-full select-none"
      style={{ width: size, height: size, font: `510 ${Math.round(size * 0.4)}px/1 var(--font)`, color: 'var(--text)', background: '#3e3e44' }}
    >
      {initials || '?'}
    </span>
  )
}
