const UNITS = ['B', 'KB', 'MB', 'GB', 'TB']

// 1536 → "1.5 KB", 5368709120 → "5 GB": binary units, one decimal under 10.
export function formatBytes(n: number) {
  let i = 0
  while (n >= 1024 && i < UNITS.length - 1) {
    n /= 1024
    i++
  }
  const shown = i === 0 || n >= 10 ? Math.round(n) : Math.round(n * 10) / 10
  return `${shown} ${UNITS[i]}`
}
