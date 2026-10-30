import { tanstackRouter } from '@tanstack/router-plugin/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

export default defineConfig({
  cacheDir: '../node_modules/.vite/user', // keep Vite's cache out of the app folder
  plugins: [
    // Must run before react(): generates src/routeTree.gen.ts and splits each route's component/loader into its own chunk.
    tanstackRouter({ target: 'react', autoCodeSplitting: true, tmpDir: '../node_modules/.tanstack/user' }),
    react(),
  ],
  resolve: { alias: { '@': '/src' } },
  server: { port: 5173, proxy: { '/api': 'http://localhost:8090' } },
})
