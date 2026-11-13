import { useEffect } from 'react'
import { Navigate, Outlet, createFileRoute, useLoaderData, useLocation, useMatch, useRouter } from '@tanstack/react-router'
import { ApiError, onMfaRequired, viewsPath, type SavedView } from '@/api'
import { loaderApi, reloadRoutes } from '@/realtime'
import { AppSidebar } from '@/components/app/sidebar'
import { guestBlocked } from '@/components/app/guest'
import { MfaBlocked } from '@/components/app/mfa-blocked'

// Pathless layout for the workspace's main pages: app sidebar + page. Settings has its own sidebar instead.
// Loads the saved views for the sidebar and the issue toolbars.
// A workspace that requires two-factor from a user without it shows MfaBlocked instead, except on Settings → Profile.
export const Route = createFileRoute('/_app/$workspace/_main')({
  loader: (ctx) =>
    loaderApi<SavedView[]>(ctx, viewsPath(ctx.params.workspace)).catch((e) => {
      if (e instanceof ApiError && e.body?.mfa_required) return []
      throw e
    }),
  component: MainLayout,
})

function MainLayout() {
  const { workspace } = Route.useParams()
  const ws = useLoaderData({ from: '/_app/$workspace' })
  const blocked = useLocation({ select: (l) => ws.guest === true && guestBlocked(l.pathname) })
  const profile = useMatch({ from: '/_app/$workspace/_main/settings/profile', shouldThrow: false })
  useStuckHeaders()
  useMfaRequired(ws.mfa_blocked === true)
  if (ws.mfa_blocked) return profile ? <Outlet /> : <MfaBlocked workspace={ws} />
  return (
    <div className="shell">
      <AppSidebar workspace={workspace} />
      {blocked ? <Navigate to="/$workspace" params={{ workspace }} replace /> : <Outlet />}
    </div>
  )
}

// A 403 {mfa_required} means the workspace started requiring two-factor: reload it so mfa_blocked shows the panel.
function useMfaRequired(blocked: boolean) {
  const router = useRouter()
  useEffect(() => {
    let reloading = false
    onMfaRequired(() => {
      if (blocked || reloading) return
      reloading = true
      void reloadRoutes(router, ['/_app/$workspace']).finally(() => (reloading = false))
    })
    return () => onMfaRequired(() => {})
  }, [router, blocked])
}

// Marks the page's sticky header data-stuck while the page has scrolled under it, so it shows its bottom border only then.
function useStuckHeaders() {
  useEffect(() => {
    let frame = 0
    const check = () => {
      frame = 0
      for (const el of document.querySelectorAll<HTMLElement>('.page-top')) {
        const stuck = scrollY > 0 && el.getBoundingClientRect().top <= parseFloat(getComputedStyle(el).top)
        el.toggleAttribute('data-stuck', stuck)
      }
    }
    const onScroll = () => void (frame ||= requestAnimationFrame(check))
    addEventListener('scroll', onScroll, { passive: true })
    addEventListener('resize', onScroll)
    return () => {
      removeEventListener('scroll', onScroll)
      removeEventListener('resize', onScroll)
      cancelAnimationFrame(frame)
    }
  }, [])
}
