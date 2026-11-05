import { useEffect } from 'react'
import { Navigate, Outlet, createFileRoute, useLoaderData, useLocation } from '@tanstack/react-router'
import { viewsPath, type SavedView } from '@/api'
import { loaderApi } from '@/realtime'
import { AppSidebar } from '@/components/app/sidebar'
import { guestBlocked } from '@/components/app/guest'

// Pathless layout for the workspace's main pages: app sidebar + page. Settings has its own sidebar instead.
// Loads the saved views for the sidebar and the issue toolbars.
export const Route = createFileRoute('/_app/$workspace/_main')({
  loader: (ctx) => loaderApi<SavedView[]>(ctx, viewsPath(ctx.params.workspace)),
  component: MainLayout,
})

function MainLayout() {
  const { workspace } = Route.useParams()
  const guest = useLoaderData({ from: '/_app/$workspace', select: (w) => w.guest })
  const blocked = useLocation({ select: (l) => guest === true && guestBlocked(l.pathname) })
  useStuckHeaders()
  return (
    <div className="shell">
      <AppSidebar workspace={workspace} />
      {blocked ? <Navigate to="/$workspace" params={{ workspace }} replace /> : <Outlet />}
    </div>
  )
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
