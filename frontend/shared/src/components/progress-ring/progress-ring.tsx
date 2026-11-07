import './progress-ring.css'

const R = 9
const C = 2 * Math.PI * R

// A small circular progress indicator in currentColor. value is 0..1; leave it out while the amount is unknown (spins).
export function ProgressRing({ value, size = 16, label }: { value?: number; size?: number; label: string }) {
  const known = value != null
  const pct = known ? Math.round(Math.min(1, Math.max(0, value)) * 100) : undefined
  return (
    <svg
      className={known ? 'progress-ring' : 'progress-ring progress-ring-spin'}
      width={size}
      height={size}
      viewBox="0 0 24 24"
      role="progressbar"
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={pct}
    >
      <circle cx="12" cy="12" r={R} className="progress-ring-track" />
      <circle cx="12" cy="12" r={R} className="progress-ring-arc" strokeDasharray={C} strokeDashoffset={C * (1 - (known ? pct! / 100 : 0.25))} />
    </svg>
  )
}

// What an upload row says: "Uploading photo.png… 40%", then "Saving photo.png…" once the bytes are sent.
export const uploadLabel = (name: string, sent?: number) =>
  sent === undefined ? `Uploading ${name}…` : sent < 1 ? `Uploading ${name}… ${Math.round(sent * 100)}%` : `Saving ${name}…`
