// Generates the video's sound with ElevenLabs into public/, where Sound.tsx picks it up:
//   music.mp3   one instrumental section per scene, each exactly as long as the scene is on screen (src/timing.ts)
//   click.mp3   the UI click when a card lands or a command is picked
// Usage, from video/:  ELEVENLABS_API_KEY=... npm run sound            (both)
//                      ELEVENLABS_API_KEY=... npm run sound -- music   (only some: music, click)
//                      npm run sound -- --dry-run                      (print the requests, call nothing)
import { writeFile } from 'node:fs/promises'
import { FPS, SCENES, SHOWN, TOTAL_FRAMES, type SceneName } from '../src/timing.ts'

const API = 'https://api.elevenlabs.io/v1'
const KEY = process.env.ELEVENLABS_API_KEY ?? ''
const args = process.argv.slice(2)
const dry = args.includes('--dry-run')
const only = args.filter((a) => !a.startsWith('--'))
const want = (name: string) => only.length === 0 || only.includes(name)

// The mood of each scene's stretch of music; one song, so these steer it rather than change genre.
const MOOD: Record<SceneName, string[]> = {
  intro: ['soft synth pad swell', 'sparse', 'building anticipation'],
  board: ['beat drops in', 'steady groove', 'plucky synth arpeggio'],
  palette: ['tight', 'crisp hi-hats', 'focused'],
  docs: ['warm', 'two melodic voices answering each other'],
  aitest: ['energy lifts', 'driving', 'confident'],
  mcp: ['futuristic', 'glitchy textures', 'forward motion'],
  cta: ['resolving', 'uplifting final chord', 'clean ending'],
}

const ms = (frames: number) => Math.round((frames / FPS) * 1000)

const music = {
  model_id: 'music_v1',
  respect_sections_durations: true,
  composition_plan: {
    positive_global_styles: ['modern tech product launch', 'electronic', 'minimal', 'polished', 'instrumental', '110 bpm'],
    negative_global_styles: ['vocals', 'lyrics', 'heavy distortion', 'orchestral', 'lo-fi hiss'],
    sections: SCENES.map(([name], i) => ({
      section_name: name,
      positive_local_styles: MOOD[name],
      negative_local_styles: [],
      duration_ms: ms(SHOWN[i]),
      lines: [],
    })),
  },
}

const effects = {
  click: { text: 'Soft satisfying UI click, a single tactile button tap, short and crisp', duration_seconds: 0.5, prompt_influence: 0.7 },
}

async function generate(path: string, body: unknown, out: string) {
  if (dry) {
    console.log(`POST ${API}${path}\n${JSON.stringify(body, null, 2)}\n`)
    return
  }
  const res = await fetch(`${API}${path}?output_format=mp3_44100_128`, {
    method: 'POST',
    headers: { 'xi-api-key': KEY, 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) throw new Error(`${out}: ElevenLabs answered ${res.status}: ${(await res.text()).slice(0, 500)}`)
  const audio = Buffer.from(await res.arrayBuffer())
  await writeFile(new URL(`../public/${out}`, import.meta.url), audio)
  console.log(`wrote public/${out} (${Math.round(audio.length / 1024)} KB)`)
}

if (!dry && !KEY) {
  console.error('Set ELEVENLABS_API_KEY (elevenlabs.io → profile → API keys), or pass --dry-run.')
  process.exit(1)
}
const total = music.composition_plan.sections.reduce((n, s) => n + s.duration_ms, 0)
console.log(`video ${ms(TOTAL_FRAMES)} ms, music sections ${total} ms`)
if (want('music')) await generate('/music', music, 'music.mp3')
if (want('click')) await generate('/sound-generation', effects.click, 'click.mp3')
