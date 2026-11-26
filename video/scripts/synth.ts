// Composes the video's sound in code, no service or key: writes public/music.wav and click.wav, which
// Sound.tsx mixes in. The music is electronic, 110 bpm, A minor (Am F C G); each scene of src/timing.ts adds or drops
// layers from the moment it starts, so the arrangement changes on the cuts.
// Usage, from video/:  npm run synth
import { writeFile } from 'node:fs/promises'
import { FPS, SCENES, START, TOTAL_FRAMES, type SceneName } from '../src/timing.ts'

const SR = 44100
const BPM = 110
const BEAT = 60 / BPM
const STEP = BEAT / 4 // a sixteenth
const BAR = BEAT * 4
const LENGTH = TOTAL_FRAMES / FPS
const TAU = Math.PI * 2

type Layer = 'pad' | 'kick' | 'bass' | 'arp' | 'hats' | 'clap' | 'lead' | 'glitch' | 'end'
const LAYERS: Record<SceneName, Layer[]> = {
  intro: ['pad'],
  board: ['pad', 'kick', 'bass', 'arp'],
  palette: ['pad', 'kick', 'bass', 'arp', 'hats'],
  docs: ['pad', 'bass', 'hats', 'lead'],
  aitest: ['pad', 'kick', 'bass', 'arp', 'hats', 'clap'],
  mcp: ['pad', 'kick', 'bass', 'glitch', 'hats', 'clap'],
  cta: ['end'],
}
const sceneStart = START.map((f) => f / FPS)
// The layers playing at time t: the scene whose start is the latest one at or before t.
function layersAt(t: number): Layer[] {
  let i = 0
  while (i + 1 < SCENES.length && sceneStart[i + 1] <= t) i++
  return LAYERS[SCENES[i][0]]
}
const on = (layer: Layer, t: number) => layersAt(t).includes(layer)

const hz = (midi: number) => 440 * 2 ** ((midi - 69) / 12)
const CHORDS = [
  { root: 45, notes: [57, 60, 64] }, // Am
  { root: 41, notes: [53, 57, 60] }, // F
  { root: 48, notes: [52, 55, 60] }, // C/E
  { root: 43, notes: [55, 59, 62] }, // G
]
const chordAt = (t: number) => CHORDS[Math.floor(t / BAR) % CHORDS.length]

const L = new Float32Array(Math.ceil(LENGTH * SR))
const R = new Float32Array(L.length)

// Adds `dur` seconds of gen(t) from `start`, panned (-1 left .. 1 right).
function add(start: number, dur: number, pan: number, gen: (t: number) => number) {
  const from = Math.max(0, Math.floor(start * SR))
  const to = Math.min(L.length, Math.floor((start + dur) * SR))
  const gl = Math.cos(((pan + 1) * Math.PI) / 4)
  const gr = Math.sin(((pan + 1) * Math.PI) / 4)
  for (let i = from; i < to; i++) {
    const v = gen(i / SR - start)
    L[i] += v * gl
    R[i] += v * gr
  }
}

const saw = (phase: number) => 2 * (phase - Math.floor(phase + 0.5))
let seed = 7
const noise = () => ((seed = (seed * 16807) % 2147483647) / 2147483647) * 2 - 1
// A one-pole low-pass, stateful per voice.
function lowpass(cutoff: number) {
  const a = 1 - Math.exp((-TAU * cutoff) / SR)
  let y = 0
  return (x: number, c?: number) => (y += (c === undefined ? a : 1 - Math.exp((-TAU * c) / SR)) * (x - y))
}
const env = (t: number, attack: number, decay: number) => (t < attack ? t / attack : Math.exp(-(t - attack) / decay))

// --- instruments ---
function pad(start: number, dur: number, notes: number[], gain: number) {
  for (const [n, midi] of notes.entries()) {
    for (const detune of [-0.08, 0.08]) {
      const f = hz(midi) * 2 ** (detune / 12)
      const lp = lowpass(1400)
      add(start, dur + 1.2, detune < 0 ? -0.5 : 0.5, (t) => {
        const amp = Math.min(1, t / 0.6) * (t > dur ? Math.exp(-(t - dur) / 0.4) : 1)
        return lp(saw(f * t + n * 0.13)) * amp * gain
      })
    }
  }
}
function kick(start: number) {
  add(start, 0.45, 0, (t) => Math.sin(TAU * (45 * t + (75 / 18) * (1 - Math.exp(-18 * t)))) * Math.exp(-t / 0.13) * 0.9)
}
function hat(start: number, open: boolean, pan: number) {
  const lp = lowpass(7000)
  add(start, open ? 0.25 : 0.06, pan, (t) => {
    const n = noise()
    return (n - lp(n)) * Math.exp(-t / (open ? 0.08 : 0.018)) * 0.22
  })
}
function clap(start: number) {
  const lp = lowpass(2500)
  add(start, 0.25, 0.1, (t) => {
    const n = noise()
    const bursts = t < 0.03 ? (Math.floor(t / 0.01) % 2 ? 0.6 : 1) : 1
    return (n - lp(n)) * Math.exp(-t / 0.07) * bursts * 0.35
  })
}
function bass(start: number, midi: number) {
  const f = hz(midi)
  const lp = lowpass(600)
  add(start, STEP * 2, 0, (t) => lp(saw(f * t), 300 + 900 * Math.exp(-t / 0.08)) * env(t, 0.005, 0.18) * 0.5)
}
function pluck(start: number, midi: number, pan: number, gain: number, crushed = false) {
  const f = hz(midi)
  const lp = lowpass(3000)
  add(start, 0.35, pan, (t) => {
    let v = lp(saw(f * t) * 0.6 + Math.sign(Math.sin(TAU * f * t)) * 0.4, 800 + 3500 * Math.exp(-t / 0.05))
    if (crushed) v = Math.round(v * 6) / 6 // a little bit-crush for the glitch scene
    return v * env(t, 0.002, 0.09) * gain
  })
}
function lead(start: number, dur: number, midi: number) {
  const f = hz(midi)
  add(start, dur + 0.3, -0.15, (t) => {
    const vib = 1 + 0.004 * Math.sin(TAU * 5.5 * t) * Math.min(1, t / 0.3)
    const tone = Math.sin(TAU * f * vib * t) + 0.25 * Math.sin(TAU * 2 * f * vib * t)
    return tone * env(t, 0.03, dur * 0.8) * 0.16
  })
}

// --- arrangement: walk the sixteenth grid and play what each scene asks for ---
const LEAD = [76, 74, 72, 69, 72, 74, 76, 79] // one note per beat, a call and answer over the chords
for (let s = 0; s * STEP < LENGTH; s++) {
  const t = s * STEP
  const inBar = s % 16
  const chord = chordAt(t)
  if (inBar === 0 && on('pad', t)) pad(t, BAR, chord.notes, 0.07)
  if (inBar % 4 === 0 && on('kick', t)) kick(t)
  if (inBar % 2 === 0 && on('bass', t)) bass(t, chord.root + (inBar % 8 === 6 ? 12 : 0))
  if (on('arp', t)) pluck(t, chord.notes[[0, 1, 2, 1][inBar % 4]] + 12, inBar % 2 ? 0.4 : -0.4, 0.11)
  if (on('glitch', t)) pluck(t, chord.notes[[0, 2, 1, 2, 0, 1][s % 6]] + (s % 5 === 0 ? 24 : 12), (s % 3) - 1, 0.1, true)
  if (on('hats', t)) hat(t, inBar % 4 === 2, inBar % 2 ? 0.3 : -0.2)
  if ((inBar === 4 || inBar === 12) && on('clap', t)) clap(t)
  if (inBar % 4 === 0 && on('lead', t)) lead(t, BEAT * 0.9, LEAD[Math.floor(s / 4) % LEAD.length])
}
// The closing card: one big C major chord from its first downbeat, ringing out to the end.
const ctaStart = sceneStart[SCENES.length - 1]
const ending = Math.ceil(ctaStart / BEAT) * BEAT
pad(ending, LENGTH - ending - 0.6, [48, 55, 60, 64, 67], 0.08)
kick(ending)
add(ending, 3, 0, (t) => Math.sin(TAU * hz(36) * t) * Math.exp(-t / 1.2) * 0.35)

// --- sound effects ---
function click(): Float32Array[] {
  const n = Math.floor(0.08 * SR)
  const mono = new Float32Array(n)
  for (let i = 0; i < n; i++) {
    const t = i / SR
    mono[i] = (Math.sin(TAU * 1800 * t) * 0.6 + noise() * 0.25) * Math.exp(-t / 0.008) * 0.8 + Math.sin(TAU * 140 * t) * Math.exp(-t / 0.02) * 0.4
  }
  return [mono, mono]
}

// --- output ---
function master(channels: Float32Array[], peak = 0.89) {
  for (const ch of channels) for (let i = 0; i < ch.length; i++) ch[i] = Math.tanh(ch[i] * 1.2) // gentle soft clip
  const max = Math.max(...channels.map((ch) => ch.reduce((m, v) => Math.max(m, Math.abs(v)), 0)))
  for (const ch of channels) for (let i = 0; i < ch.length; i++) ch[i] = (ch[i] / (max || 1)) * peak
  return channels
}
function wav([l, r]: Float32Array[]) {
  const data = Buffer.alloc(l.length * 4)
  for (let i = 0; i < l.length; i++) {
    data.writeInt16LE(Math.round(l[i] * 32767), i * 4)
    data.writeInt16LE(Math.round(r[i] * 32767), i * 4 + 2)
  }
  const head = Buffer.alloc(44)
  head.write('RIFF', 0)
  head.writeUInt32LE(36 + data.length, 4)
  head.write('WAVEfmt ', 8)
  head.writeUInt32LE(16, 16)
  head.writeUInt16LE(1, 20) // PCM
  head.writeUInt16LE(2, 22) // stereo
  head.writeUInt32LE(SR, 24)
  head.writeUInt32LE(SR * 4, 28)
  head.writeUInt16LE(4, 32)
  head.writeUInt16LE(16, 34)
  head.write('data', 36)
  head.writeUInt32LE(data.length, 40)
  return Buffer.concat([head, data])
}
const save = async (name: string, channels: Float32Array[]) => {
  const file = wav(channels)
  await writeFile(new URL(`../public/${name}`, import.meta.url), file)
  console.log(`wrote public/${name} (${(channels[0].length / SR).toFixed(2)} s, ${Math.round(file.length / 1024)} KB)`)
}
await save('music.wav', master([L, R]))
await save('click.wav', master(click(), 0.8))
