import { useId, useState, type CSSProperties, type KeyboardEvent, type PointerEvent } from 'react'
import { useFieldError } from '../input/field-error'
import { Popover } from '../popover/popover'
import { HEX, parseHex, toHex, type HSVA } from './color'
import '../input/input.css'
import './color-picker.css'

type ColorPickerProps = {
  label?: string
  'aria-label'?: string
  value: string
  onChange: (value: string) => void
  name?: string
  error?: string | null
  disabled?: boolean
  swatches?: string[]
}

export const SWATCHES = ['#5e6ad2', '#f91880', '#00ba7c', '#f2994a', '#f2c94c', '#26b5ce', '#bb87fc', '#8a8f98']

// Field showing a hex color ('#rrggbb', or '#rrggbbaa' below full opacity) that opens a picker: saturation and
// brightness, hue, opacity, the hex itself and swatches. `name` submits it with a form.
export function ColorPicker({ label, 'aria-label': ariaLabel, value, onChange, name, error: rawError, disabled, swatches = SWATCHES }: ColorPickerProps) {
  const id = useId()
  const errorId = `${id}-error`
  const [error, onEdit] = useFieldError(rawError, value)
  const change = (hex: string) => {
    onEdit()
    onChange(hex)
  }

  return (
    <div className="grid gap-2">
      {label ? <label htmlFor={id} className="text-label text-secondary">{label}</label> : null}
      <Popover
        aria-label={label ?? ariaLabel ?? 'Color'}
        arrow={false}
        trigger={(props) => (
          <button
            {...props}
            id={id}
            type="button"
            className="input color-picker-trigger"
            aria-label={label ? undefined : ariaLabel}
            aria-haspopup="dialog"
            aria-invalid={error ? true : undefined}
            aria-describedby={error ? errorId : undefined}
            disabled={disabled}
          >
            <Swatch color={value} />
            <span className="ellipsis color-picker-value">{value}</span>
          </button>
        )}
      >
        <Panel value={value} onChange={change} swatches={swatches} />
      </Popover>
      {name ? <input type="hidden" name={name} value={value} /> : null}
      {error ? <span id={errorId} className="text-caption text-accent">{error}</span> : null}
    </div>
  )
}

function Swatch({ color }: { color: string }) {
  return (
    <span aria-hidden className="color-picker-swatch">
      <span style={{ background: color }} />
    </span>
  )
}

function Panel({ value, onChange, swatches }: { value: string; onChange: (hex: string) => void; swatches: string[] }) {
  // HSVA is kept here, not derived from the hex, so the hue survives dragging to gray or black.
  const [hsva, setHsva] = useState<HSVA>(() => parseHex(value) ?? { h: 0, s: 0, v: 0, a: 1 })
  const [text, setText] = useState(value)
  const [seen, setSeen] = useState(value)
  if (value !== seen) {
    setSeen(value)
    setText(value)
    if (value !== toHex(hsva)) setHsva(parseHex(value) ?? hsva)
  }
  const set = (next: HSVA) => {
    setHsva(next)
    const hex = toHex(next)
    setText(hex)
    if (hex !== value) onChange(hex)
  }
  const opaque = toHex({ ...hsva, a: 1 })

  const pick = (e: PointerEvent<HTMLDivElement>) => {
    if (e.buttons !== 1) return
    const r = e.currentTarget.getBoundingClientRect()
    const clamp = (n: number) => Math.min(1, Math.max(0, n))
    set({ ...hsva, s: clamp((e.clientX - r.left) / r.width), v: clamp(1 - (e.clientY - r.top) / r.height) })
  }
  const nudge = (e: KeyboardEvent<HTMLDivElement>) => {
    const step = e.shiftKey ? 0.1 : 0.01
    const moves: Record<string, [number, number]> = { ArrowLeft: [-step, 0], ArrowRight: [step, 0], ArrowDown: [0, -step], ArrowUp: [0, step] }
    const d = moves[e.key]
    if (!d) return
    e.preventDefault()
    const clamp = (n: number) => Math.min(1, Math.max(0, n))
    set({ ...hsva, s: clamp(hsva.s + d[0]), v: clamp(hsva.v + d[1]) })
  }

  return (
    <div className="color-picker">
      <div
        className="color-picker-area"
        style={{ backgroundColor: `hsl(${hsva.h} 100% 50%)` }}
        role="slider"
        tabIndex={0}
        aria-label="Saturation and brightness"
        aria-valuetext={`Saturation ${Math.round(hsva.s * 100)}%, brightness ${Math.round(hsva.v * 100)}%`}
        onPointerDown={(e) => {
          e.currentTarget.setPointerCapture(e.pointerId)
          pick(e)
        }}
        onPointerMove={pick}
        onKeyDown={nudge}
      >
        <span className="color-picker-thumb" style={{ left: `${hsva.s * 100}%`, top: `${(1 - hsva.v) * 100}%`, background: opaque }} />
      </div>
      <input
        type="range"
        className="color-picker-slider color-picker-hue"
        aria-label="Hue"
        min={0}
        max={360}
        value={Math.round(hsva.h)}
        onChange={(e) => set({ ...hsva, h: Number(e.target.value) })}
      />
      <input
        type="range"
        className="color-picker-slider color-picker-alpha"
        style={{ '--color': opaque } as CSSProperties}
        aria-label="Opacity"
        aria-valuetext={`${Math.round(hsva.a * 100)}%`}
        min={0}
        max={100}
        value={Math.round(hsva.a * 100)}
        onChange={(e) => set({ ...hsva, a: Number(e.target.value) / 100 })}
      />
      <div className="flex items-center gap-2">
        <input
          className="input color-picker-hex"
          aria-label="Hex"
          spellCheck={false}
          maxLength={9}
          value={text}
          onChange={(e) => {
            const v = e.target.value.trim().toLowerCase()
            setText(v)
            const next = HEX.test(v) ? parseHex(v) : null
            if (next) set(next)
          }}
          onBlur={() => setText(toHex(hsva))}
        />
        <span className="text-caption text-secondary color-picker-percent">{Math.round(hsva.a * 100)}%</span>
      </div>
      <div className="color-picker-swatches">
        {swatches.map((s) => (
          <button key={s} type="button" className="color-picker-preset" aria-label={s} aria-pressed={s === value} onClick={() => set(parseHex(s) ?? hsva)}>
            <Swatch color={s} />
          </button>
        ))}
      </div>
    </div>
  )
}
