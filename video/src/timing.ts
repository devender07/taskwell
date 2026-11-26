// The video's timeline, shared by the composition (Launch.tsx) and the sound generator (scripts/sound.ts):
// each scene's name and length in frames at FPS; neighbours overlap by FADE frames while one fades into the next.
export const FPS = 60
export const FADE = 20
export const SCENES = [
  ['intro', 210],
  ['board', 390],
  ['palette', 270],
  ['docs', 300],
  ['aitest', 330],
  ['mcp', 300],
  ['cta', 270],
] as const
export type SceneName = (typeof SCENES)[number][0]

export const TOTAL_FRAMES = SCENES.reduce((n, [, d]) => n + d, 0) - FADE * (SCENES.length - 1)
// The frame each scene starts on, and so where each fade begins.
export const START = SCENES.map((_, i) => SCENES.slice(0, i).reduce((n, [, d]) => n + d - FADE, 0))
// How long each scene is on screen on its own: up to where the next one starts.
export const SHOWN = SCENES.map(([, d], i) => (i < SCENES.length - 1 ? d - FADE : d))
