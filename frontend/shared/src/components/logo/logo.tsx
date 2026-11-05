// Taskwell mark: a T whose stem is a slack cord, held at a pink knot.
export function LogoMark({ size = 40 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 40 40" role="img" aria-label="Taskwell">
      <rect width="40" height="40" rx="9" fill="#F7F8F8" />
      <path d="M11 13H29" stroke="#08090A" strokeWidth="4" strokeLinecap="round" />
      <path d="M20 13C20 20.5 16 22.5 17.5 29.5" stroke="#08090A" strokeWidth="4" strokeLinecap="round" fill="none" />
      <circle cx="20" cy="13" r="3.6" fill="#F91880" stroke="#F7F8F8" strokeWidth="1.5" />
    </svg>
  )
}

export function Logo({ size = 28 }: { size?: number }) {
  return (
    <span className="inline-flex items-center gap-2">
      <LogoMark size={size} />
      <span aria-hidden style={{ font: `590 ${Math.round(size * 0.7)}px/1 var(--font)` }}>
        Taskwell
      </span>
    </span>
  )
}
