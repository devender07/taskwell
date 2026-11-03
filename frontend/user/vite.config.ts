import { tanstackRouter } from '@tanstack/router-plugin/vite'
import react from '@vitejs/plugin-react'
import { createLogger, defineConfig, type Plugin } from 'vite'

// A tab that reloads or closes mid-message resets its proxied WebSocket; Vite logs that as an error with a stack
// trace. The tab reconnects on its own, so drop just those two codes.
const logger = createLogger()
const logError = logger.error
logger.error = (msg, options) => {
  if (msg.includes('ws proxy socket error') && /\b(ECONNRESET|EPIPE)\b/.test(msg)) return
  logError(msg, options)
}

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

export default defineConfig({
  customLogger: logger,
  cacheDir: '../node_modules/.vite/user', // keep Vite's cache out of the app folder
  plugins: [
    // Must run before react(): generates src/routeTree.gen.ts and splits each route's component/loader into its own chunk.
    tanstackRouter({ target: 'react', autoCodeSplitting: true, tmpDir: '../node_modules/.tanstack/user' }),
    react(),
    landingAtRoot,
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
  server: {
    host: true,
    port: 5173,
    proxy: {
      '/api': { target: 'http://localhost:8090', ws: true },
      '/mcp': 'http://localhost:8095',
      '/.well-known/oauth-protected-resource': 'http://localhost:8095',
      '/.well-known/oauth-authorization-server': 'http://localhost:8090',
    },
  },
})
