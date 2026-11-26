import { continueRender, delayRender, staticFile } from 'remotion'

// Taskwell's dark theme (DESIGN.md / frontend/shared tokens.css).
export const C = {
  bg: '#08090A',
  surface: '#0F1011',
  raised: '#16171A',
  text: '#F7F8F8',
  text2: '#8A8F98',
  border: 'rgba(255, 255, 255, 0.08)',
  accent: '#F91880',
  success: '#00BA7C',
  link: '#5E6AD2',
  yellow: '#F2C94C',
}

export const FONT = '"Plus Jakarta Sans", system-ui, sans-serif'
export const MONO = '"Berkeley Mono", ui-monospace, "SF Mono", Menlo, Consolas, monospace'

// The render waits for the brand font, so no frame is drawn in a fallback face.
const fontReady = delayRender('Plus Jakarta Sans')
const face = new FontFace('Plus Jakarta Sans', `url(${staticFile('PlusJakartaSans-latin-normal.woff2')}) format('woff2')`, {
  weight: '200 800',
})
face
  .load()
  .then((f) => {
    document.fonts.add(f)
    continueRender(fontReady)
  })
  .catch(() => continueRender(fontReady))
