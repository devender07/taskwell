# Taskwell launch video

A 32-second, 60 fps launch video, 1920×1080 and as a 1080×1920 reel, made with [Remotion](https://www.remotion.dev) (React). It's its own npm
project, outside the frontend workspaces.

```sh
cd video
npm install
npm run studio    # preview and scrub in the browser
npm run render         # out/taskwell-launch.mp4: 16:9 for LinkedIn feeds and YouTube (H.264, CRF 16)
npm run render:reel    # out/taskwell-reel.mp4: 9:16 for phones (LinkedIn, Instagram, YouTube Shorts)
npm run still     # out/frame.png, one frame for a thumbnail
```

## AI sound

`npm run sound` generates the sound with ElevenLabs into `public/`, then `npm run render` mixes it in:

```sh
ELEVENLABS_API_KEY=sk_... npm run sound            # music.mp3, click.mp3
ELEVENLABS_API_KEY=sk_... npm run sound -- music   # regenerate only one (music or click)
npm run sound -- --dry-run                         # print the requests without calling anything
```

The music is one instrumental track with a section per scene, each exactly as long as the scene is on screen, so its
changes land on the cuts. Change a scene's mood in `MOOD` in `scripts/sound.ts`, or a scene's length in
`src/timing.ts` (the video and the music both follow it). Every file is optional: without one, that sound is left out.

- `src/narration.json`: the narrator's lines (`npm run voice` speaks them).
- `src/timing.ts`: the scenes' order and lengths, shared by the video and the sound.
- `src/Sound.tsx`: where the music, voice and clicks play, and how loud.
- `src/scenes.tsx`: the seven scenes (intro, board, command palette, live docs, AI testing, MCP, call to action).
- `src/Launch.tsx`: the scenes joined by 20-frame fades.
- `src/theme.ts`: Taskwell's colours and font (Plus Jakarta Sans from `public/`).
- `remotion.config.ts`: the render settings.

Remotion is free for individuals and companies of up to 3 people; see its license before a bigger team uses it.
