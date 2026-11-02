import { tanstackRouter } from '@tanstack/router-plugin/vite'
import react from '@vitejs/plugin-react'
import { createLogger, defineConfig } from 'vite'

// A tab that reloads or closes mid-message resets its proxied WebSocket; Vite logs that as an error with a stack
// trace. The tab reconnects on its own, so drop just those two codes.
const logger = createLogger()
const logError = logger.error
logger.error = (msg, options) => {
  if (msg.includes('ws proxy socket error') && /\b(ECONNRESET|EPIPE)\b/.test(msg)) return
  logError(msg, options)
}

export default defineConfig({
  customLogger: logger,
  cacheDir: '../node_modules/.vite/user', // keep Vite's cache out of the app folder
  plugins: [
    // Must run before react(): generates src/routeTree.gen.ts and splits each route's component/loader into its own chunk.
    tanstackRouter({ target: 'react', autoCodeSplitting: true, tmpDir: '../node_modules/.tanstack/user' }),
    react(),
  ],
  resolve: { alias: { '@': '/src' } },
  build: {
    target: ['safari15.4', 'chrome111', 'firefox114', 'edge111'], // the oldest browsers supported
    // The editor's vendors, each under Vite's 500 kB warning; only issue pages and the create dialog load them. React
    // gets its own chunk, or Rollup would pull it into tiptap's (a dependency of both) and every page would load that.
    rollupOptions: {
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
  server: { host: true, port: 5173, proxy: { '/api': { target: 'http://localhost:8090', ws: true }, '/mcp': 'http://localhost:8095' } },
})
