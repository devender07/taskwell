const DAY = 86_400_000

// "just now", "5m ago", "3h ago", "yesterday", then a short date ("Oct 12", "Oct 12, 2025" in other years) in local
// time and the browser's locale, like every other date in the app. `locale` is for tests.
export function relativeTime(iso: string, now = new Date(), locale?: string): string {
  const d = new Date(iso)
  const ms = now.getTime() - d.getTime()
  if (ms < 60_000) return 'just now'
  if (ms < 3_600_000) return `${Math.floor(ms / 60_000)}m ago`
  if (ms < DAY) return `${Math.floor(ms / 3_600_000)}h ago`
  const yesterday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1)
  if (d >= yesterday) return 'yesterday'
  const year = d.getFullYear() === now.getFullYear() ? undefined : 'numeric'
  return d.toLocaleDateString(locale, { month: 'short', day: 'numeric', year })
}

// The full local date and time, for a title tooltip next to a relative time.
export const fullTime = (iso: string) => new Date(iso).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
