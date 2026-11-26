import { Audio, getStaticFiles, interpolate, Sequence, staticFile, useVideoConfig } from 'remotion'

// Optional sound, each used only when it's in public/ (.mp3 or .wav; npm run synth or npm run sound make them):
//   music   the soundtrack, faded in over 1s and out over the last 2s, trimmed to the video
//   click   when the board card lands and when the palette picks "Create issue"
//   voice/<scene>  the narration line for each scene (npm run voice), VOICE_DELAY frames after the scene starts;
//                  while there's narration the music sits lower so the voice stays clear
const file = (base: string) => [`${base}.mp3`, `${base}.wav`].find((name) => getStaticFiles().some((f) => f.name === name))

const VOICE_DELAY = 15

export function Sound({ clicks, scenes }: { clicks: number[]; scenes: { name: string; at: number }[] }) {
  const { durationInFrames, fps } = useVideoConfig()
  const voices = scenes.flatMap(({ name, at }) => {
    const src = file(`voice/${name}`)
    return src ? [{ src, at: at + VOICE_DELAY }] : []
  })
  const bed = voices.length ? 0.22 : 0.6
  const music = (f: number) =>
    interpolate(f, [0, fps, durationInFrames - 2 * fps, durationInFrames], [0, bed, bed, 0], { extrapolateLeft: 'clamp', extrapolateRight: 'clamp' })
  const [music_, click] = [file('music'), file('click')]
  return (
    <>
      {voices.map(({ src, at }) => (
        <Sequence key={src} from={at}>
          <Audio src={staticFile(src)} volume={1} />
        </Sequence>
      ))}
      {music_ ? <Audio src={staticFile(music_)} volume={music} /> : null}
      {click
        ? clicks.map((at) => (
            <Sequence key={`c${at}`} from={at} durationInFrames={fps / 2}>
              <Audio src={staticFile(click)} volume={0.5} />
            </Sequence>
          ))
        : null}
    </>
  )
}
