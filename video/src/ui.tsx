import type { CSSProperties, ReactNode } from 'react'
import { AbsoluteFill, interpolate, spring, useCurrentFrame, useVideoConfig } from 'remotion'
import { C, FONT, MONO } from './theme'

// The reel (9:16) stacks each scene: headline at the top, the window full width below it.
export function useLayout() {
  const { width, height } = useVideoConfig()
  return { W: width, H: height, portrait: height > width }
}

// 0 → 1 with a soft spring, starting `delay` frames into the scene.
export function useIn(delay = 0, damping = 18) {
  const frame = useCurrentFrame()
  const { fps } = useVideoConfig()
  return spring({ frame: frame - delay, fps, config: { damping, mass: 0.8 } })
}

// Fades and lifts its children in.
export function Rise({ delay = 0, distance = 24, children, style }: { delay?: number; distance?: number; children: ReactNode; style?: CSSProperties }) {
  const p = useIn(delay)
  return <div style={{ opacity: p, transform: `translateY(${(1 - p) * distance}px)`, ...style }}>{children}</div>
}

// The first `chars` characters of text appear one by one from `start`, `perChar` frames each.
export function typed(text: string, frame: number, start: number, perChar = 2) {
  return text.slice(0, Math.max(0, Math.floor((frame - start) / perChar)))
}

export function Caret({ color = C.link }: { color?: string }) {
  const frame = useCurrentFrame()
  return <span style={{ display: 'inline-block', width: 3, height: '1em', marginLeft: 4, verticalAlign: '-0.12em', background: color, opacity: Math.floor(frame / 30) % 2 ? 0 : 1 }} />
}

export function Scene({ children }: { children: ReactNode }) {
  return (
    <AbsoluteFill style={{ background: C.bg, fontFamily: FONT, color: C.text }}>
      <AbsoluteFill style={{ background: 'radial-gradient(ellipse 60% 50% at 50% 0%, rgba(94, 106, 210, 0.16), transparent 70%)' }} />
      {children}
    </AbsoluteFill>
  )
}

// The scene's headline at the top: a small label with a pink dot, then the line.
export function Caption({ eyebrow, title }: { eyebrow: string; title: string }) {
  const { portrait } = useLayout()
  return (
    <div style={{ position: 'absolute', top: portrait ? 200 : 96, left: 60, right: 60, display: 'grid', justifyItems: 'center', gap: portrait ? 28 : 20, textAlign: 'center' }}>
      <Rise>
        <div style={{ display: 'inline-flex', alignItems: 'center', gap: 12, fontSize: portrait ? 34 : 26, fontWeight: 510, color: C.text2 }}>
          <span style={{ width: 12, height: 12, borderRadius: 99, background: C.accent, boxShadow: '0 0 0 6px rgba(249, 24, 128, 0.16)' }} />
          {eyebrow}
        </div>
      </Rise>
      <Rise delay={6}>
        <div style={{ fontSize: portrait ? 76 : 64, fontWeight: 590, letterSpacing: -1.5, lineHeight: 1.1 }}>{title}</div>
      </Rise>
    </div>
  )
}

// A dark app window with traffic-light dots and a title; in the reel it's full width, below the headline.
export function Window({ title, width, height, tall, children, delay = 12, style }: { title: string; width: number; height: number; tall?: number; children: ReactNode; delay?: number; style?: CSSProperties }) {
  const p = useIn(delay, 20)
  const { W, portrait } = useLayout()
  const w = portrait ? W - 64 : width
  return (
    <div
      style={{
        position: 'absolute',
        left: (W - w) / 2,
        top: portrait ? 560 : 330,
        width: w,
        height: portrait ? (tall ?? height) : height,
        background: C.surface,
        border: `1px solid ${C.border}`,
        borderRadius: 16,
        overflow: 'hidden',
        boxShadow: '0 0 0 1px rgba(0,0,0,0.6), 0 40px 120px rgba(94, 106, 210, 0.18)',
        opacity: p,
        transform: `translateY(${(1 - p) * 60}px) scale(${interpolate(p, [0, 1], [0.96, 1])})`,
        ...style,
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, height: 56, padding: '0 22px', borderBottom: `1px solid ${C.border}` }}>
        {[0, 1, 2].map((i) => (
          <span key={i} style={{ width: 14, height: 14, borderRadius: 99, background: 'rgba(255,255,255,0.1)' }} />
        ))}
        <span style={{ margin: '0 auto', fontSize: 20, color: C.text2 }}>{title}</span>
      </div>
      {children}
    </div>
  )
}

export function Tag({ children, tone = 'plain' }: { children: ReactNode; tone?: 'plain' | 'urgent' | 'pass' }) {
  const tones = {
    plain: { color: C.text2, border: C.border, background: 'transparent' },
    urgent: { color: '#fff', border: 'transparent', background: 'rgba(249, 24, 128, 0.28)' },
    pass: { color: C.success, border: 'rgba(0, 186, 124, 0.4)', background: 'transparent' },
  }[tone]
  return <span style={{ padding: '2px 12px', fontSize: 17, borderRadius: 99, border: `1px solid ${tones.border}`, color: tones.color, background: tones.background }}>{children}</span>
}

export function Key({ children }: { children: ReactNode }) {
  return <kbd style={{ padding: '2px 10px', fontFamily: MONO, fontSize: 18, color: C.text2, border: `1px solid ${C.border}`, borderRadius: 6 }}>{children}</kbd>
}

export function Logo({ size = 96 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 40 40" aria-hidden>
      <rect width="40" height="40" rx="9" fill={C.text} />
      <path d="M11 13H29" stroke={C.bg} strokeWidth="4" strokeLinecap="round" />
      <path d="M20 13C20 20.5 16 22.5 17.5 29.5" stroke={C.bg} strokeWidth="4" strokeLinecap="round" fill="none" />
      <circle cx="20" cy="13" r="3.6" fill={C.accent} stroke={C.text} strokeWidth="1.5" />
    </svg>
  )
}
