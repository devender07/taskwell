import { useState } from 'react'

// Initials in a circle, covered by the photo at `src` when it loads.
export function Avatar({ name, size = 32, src }: { name: string; size?: number; src?: string | null }) {
  const [failed, setFailed] = useState<string | null>(null)
  const initials = name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((word) => word[0]!.toUpperCase())
    .join('')

  return (
    <span
      aria-hidden
      className="relative inline-grid place-center rounded-full select-none flex-noshrink overflow-hidden"
      style={{ width: size, height: size, font: `510 ${Math.round(size * 0.4)}px/1 var(--font)`, color: 'var(--text)', background: '#3e3e44' }}
    >
      {initials || '?'}
      {src && failed !== src ? (
        <img
          src={src}
          alt=""
          className="absolute"
          style={{ inset: 0, width: '100%', height: '100%', objectFit: 'cover', background: '#3e3e44' }}
          onError={() => setFailed(src)}
        />
      ) : null}
    </span>
  )
}
