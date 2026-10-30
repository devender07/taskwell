import { Outlet, createFileRoute, notFound } from '@tanstack/react-router'
import { ApiError, api, type Workspace } from '@/api'

// Children read the workspace with useLoaderData({ from: '/_app/$workspace' }); a 404 renders not found.
export const Route = createFileRoute('/_app/$workspace')({
  loader: async ({ params }) => {
    try {
      return await api<Workspace>(`/workspaces/${encodeURIComponent(params.workspace)}`)
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) throw notFound()
      throw e
    }
  },
  component: Outlet,
})
