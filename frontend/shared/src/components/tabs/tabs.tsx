import type { CSSProperties } from 'react'
import './tabs.css'

type TabsProps<T extends string> = {
  items: { value: T; label: string }[]
  value: T
  onChange: (value: T) => void
  'aria-label': string
  className?: string
}

export function Tabs<T extends string>({ items, value, onChange, className, ...props }: TabsProps<T>) {
  const index = Math.max(0, items.findIndex((item) => item.value === value))

  return (
    <div
      role="group"
      className={className ? `tabs ${className}` : 'tabs'}
      style={{ '--n': items.length, '--i': index } as CSSProperties}
      {...props}
    >
      <span className="tabs-indicator" aria-hidden />
      {items.map((item) => (
        <button key={item.value} type="button" className="tab" aria-pressed={item.value === value} onClick={() => onChange(item.value)}>
          {item.label}
        </button>
      ))}
    </div>
  )
}
