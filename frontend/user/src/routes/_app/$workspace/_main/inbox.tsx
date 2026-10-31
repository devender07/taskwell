import { Outlet, createFileRoute, useLoaderData, useMatch } from '@tanstack/react-router'
import type { Chat, Member } from '@/api'
import { loaderApi } from '@/realtime'
import { ChatList } from '@/components/chat/chat-list'

type Search = { new?: 'group' | 'message' }

export const Route = createFileRoute('/_app/$workspace/_main/inbox')({
  validateSearch: (search): Search => (search.new === 'group' || search.new === 'message' ? { new: search.new } : {}),
  loader: async (ctx) => {
    const path = `/workspaces/${encodeURIComponent(ctx.params.workspace)}`
    const [chats, { members }] = await Promise.all([loaderApi<Chat[]>(ctx, `${path}/chats`), loaderApi<{ members: Member[] }>(ctx, `${path}/members`)])
    return { chats, members }
  },
  component: Inbox,
})

// The chat list on the left and the open chat (or Activity) beside it; below 1024px one replaces the other.
function Inbox() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const { user } = Route.useRouteContext()
  const { chats, members } = Route.useLoaderData()
  const search = Route.useSearch()
  const onIndex = Boolean(useMatch({ from: '/_app/$workspace/_main/inbox/', shouldThrow: false }))
  return (
    <main className="chat-page" data-open={onIndex ? undefined : ''}>
      <ChatList slug={workspace.slug} workspaceName={workspace.name} user={user} chats={chats} members={members} dialog={search.new} />
      <Outlet />
    </main>
  )
}
