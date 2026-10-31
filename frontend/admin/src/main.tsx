import { RouterProvider } from '@tanstack/react-router'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { router } from './router'
import '@tether/shared/tokens.css'
import '@tether/shared/utilities.css'

// After a deploy, an old tab's lazy chunks are gone: reload to get the new ones, at most once a minute.
addEventListener('vite:preloadError', (e) => {
  try {
    if (Date.now() - Number(sessionStorage.getItem('tether:reloaded')) < 60_000) return
    sessionStorage.setItem('tether:reloaded', String(Date.now()))
  } catch {
    return
  }
  e.preventDefault()
  location.reload()
})

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <RouterProvider router={router} />
  </StrictMode>,
)
