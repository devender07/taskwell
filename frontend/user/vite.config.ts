import { tanstackRouter } from '@tanstack/router-plugin/vite'
import react from '@vitejs/plugin-react'
import { defineConfig, type Plugin } from 'vite'

// / is the static landing page (landing.html, for search engines); every other path is the app. Caddy does the same
// in production.
const landingAtRoot: Plugin = {
  name: 'landing-at-root',
  configureServer: (server) => void server.middlewares.use(toLanding),
  configurePreviewServer: (server) => void server.middlewares.use(toLanding),
}
function toLanding(req: { url?: string }, _res: unknown, next: () => void) {
  if (req.url === '/' || req.url?.startsWith('/?')) req.url = '/landing.html' + req.url.slice(1)
  next()
}

// Cloudflare Pages headers (what the Caddyfile sets): the CSP lets the app reach VITE_API_URL over https and wss.
// Images come from the API or its storage redirect, so img-src allows any https origin.
let api = ''
const pagesHeaders: Plugin = {
  name: 'pages-headers',
  apply: 'build',
  configResolved: (config) => void (api = new URL(config.env.VITE_API_URL).origin),
  generateBundle() {
    const csp = `default-src 'self'; img-src 'self' data: blob: https:; media-src 'self' https:; style-src 'self' 'unsafe-inline'; connect-src 'self' ${api} ${api.replace(/^http/, 'ws')}; frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'`
    this.emitFile({
      type: 'asset',
      fileName: '_headers',
      source: `/*
  Content-Security-Policy: ${csp}
  Strict-Transport-Security: max-age=31536000; includeSubDomains
  Referrer-Policy: no-referrer
  X-Content-Type-Options: nosniff
  Permissions-Policy: camera=(), microphone=(), geolocation=(), payment=(), usb=()

/assets/*
  Cache-Control: public, max-age=31536000, immutable
`,
    })
  },
}

export default defineConfig({
  cacheDir: '../node_modules/.vite/user', // keep Vite's cache out of the app folder
  plugins: [
    // Must run before react(): generates src/routeTree.gen.ts and splits each route's component/loader into its own chunk.
    tanstackRouter({ target: 'react', autoCodeSplitting: true, tmpDir: '../node_modules/.tanstack/user' }),
    react(),
    landingAtRoot,
    pagesHeaders,
  ],
  resolve: { alias: { '@': '/src' } },
  build: {
    target: ['safari15.4', 'chrome111', 'firefox114', 'edge111'], // the oldest browsers supported
    // The editor's vendors, each under Vite's 500 kB warning; only issue pages and the create dialog load them. React
    // gets its own chunk, or Rollup would pull it into tiptap's (a dependency of both) and every page would load that.
    rollupOptions: {
      input: { app: 'index.html', landing: 'landing.html' },
      output: {
        manualChunks(id) {
          if (/\/node_modules\/(react|react-dom|scheduler|use-sync-external-store)\//.test(id)) return 'react'
          if (id.includes('/node_modules/prosemirror-')) return 'prosemirror'
          if (id.includes('/node_modules/@tiptap/')) return 'tiptap'
          if (/\/node_modules\/(yjs|y-protocols|lib0)\//.test(id)) return 'yjs'
        },
      },
    },
  },
  server: { host: true, port: 5173 },
})
