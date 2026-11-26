import { linearTiming, TransitionSeries } from '@remotion/transitions'
import { fade } from '@remotion/transitions/fade'
import type { FC } from 'react'
import { AiTest, Board, Cta, Docs, Intro, Mcp, Palette } from './scenes'
import { Sound } from './Sound'
import { FADE, SCENES, START, TOTAL_FRAMES, type SceneName } from './timing'

const VIEWS: Record<SceneName, FC> = { intro: Intro, board: Board, palette: Palette, docs: Docs, aitest: AiTest, mcp: Mcp, cta: Cta }

export const LAUNCH_FRAMES = TOTAL_FRAMES
// Board card lands in In progress and in Done (scenes.tsx: springs at 80 and 190); the palette picks at 130.
const CLICKS = [START[1] + 100, START[1] + 210, START[2] + 130]

export function Launch() {
  return (
    <>
      <Sound clicks={CLICKS} scenes={SCENES.map(([name], i) => ({ name, at: START[i] }))} />
      <TransitionSeries>
        {SCENES.flatMap(([name, frames], i) => {
          const View = VIEWS[name]
          return [
            ...(i ? [<TransitionSeries.Transition key={`t${i}`} presentation={fade()} timing={linearTiming({ durationInFrames: FADE })} />] : []),
            <TransitionSeries.Sequence key={name} durationInFrames={frames}>
              <View />
            </TransitionSeries.Sequence>,
          ]
        })}
      </TransitionSeries>
    </>
  )
}
