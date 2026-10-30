const DAY = 86_400_000

// "just now", "5m ago", "3h ago", "yesterday", "Oct 12", "Oct 12, 2025" (other years), in local time.
export function relativeTime(iso: string, now = new Date()): string {
  const d = new Date(iso)
  const ms = now.getTime() - d.getTime()
  if (ms < 60_000) return 'just now'
  if (ms < 3_600_000) return `${Math.floor(ms / 60_000)}m ago`
  if (ms < DAY) return `${Math.floor(ms / 3_600_000)}h ago`
  const yesterday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1)
  if (d >= yesterday) return 'yesterday'
  const year = d.getFullYear() === now.getFullYear() ? undefined : 'numeric'
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric', year })
}

// The full local date and time, for a title tooltip next to a relative time.
export const fullTime = (iso: string) => new Date(iso).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
