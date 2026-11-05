import { Tabs } from '@taskwell/shared/tabs'
import { setTheme, useTheme, type Theme } from '@/components/app/theme'

const items: { value: Theme; label: string }[] = [
  { value: 'system', label: 'System' },
  { value: 'light', label: 'Light' },
  { value: 'dark', label: 'Dark' },
]

export function AppearanceSetting() {
  const { theme } = useTheme()
  return (
    <div className="grid gap-3">
      <h2 className="text-label">Appearance</h2>
      <Tabs aria-label="Theme" className="w-fit" items={items} value={theme} onChange={setTheme} />
      <p className="text-caption text-secondary">System follows your device's light or dark setting. Saved in this browser.</p>
    </div>
  )
}
