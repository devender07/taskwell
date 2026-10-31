import { useState } from 'react'
import { Checkbox } from '@tether/shared/checkbox'
import { setSingleKeyShortcuts, singleKeyShortcuts } from '@/components/app/shortcuts'

export function ShortcutsSetting() {
  const [on, setOn] = useState(singleKeyShortcuts)
  return (
    <div className="grid gap-3">
      <h2 className="text-label">Keyboard shortcuts</h2>
      <Checkbox
        checked={on}
        onChange={(e) => {
          setSingleKeyShortcuts(e.target.checked)
          setOn(e.target.checked)
        }}
        label="Single-key shortcuts"
      />
      <p className="text-caption text-secondary">
        C, /, ?, S, A, P and L work without a modifier key. Turn them off if you use speech input or press them by accident. The command palette stays on Ctrl+K or ⌘K. Saved in this browser.
      </p>
    </div>
  )
}
