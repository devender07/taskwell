import { Composition } from 'remotion'
import { Launch, LAUNCH_FRAMES } from './Launch'
import './theme'

// Launch: 16:9 for LinkedIn feeds and YouTube. Reel: 9:16 for phones (LinkedIn, Instagram, YouTube Shorts), the same
// scenes stacked.
export function Root() {
  return (
    <>
      <Composition id="Launch" component={Launch} durationInFrames={LAUNCH_FRAMES} fps={60} width={1920} height={1080} />
      <Composition id="Reel" component={Launch} durationInFrames={LAUNCH_FRAMES} fps={60} width={1080} height={1920} />
    </>
  )
}
