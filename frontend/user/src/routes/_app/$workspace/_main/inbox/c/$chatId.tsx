import { createFileRoute, notFound, useLoaderData } from '@tanstack/react-router'
import { ApiError, type Chat, type MessagePage, workspacePath } from '@/api'
import { loaderApi } from '@/realtime'
import { ChatView } from '@/components/chat/chat-view'

type Search = { thread?: string; message?: string; people?: boolean }
const id = (v: unknown) => (typeof v === 'string' && v ? v : undefined)

export const Route = createFileRoute('/_app/$workspace/_main/inbox/c/$chatId')({
  validateSearch: (search): Search => {
    const out: Search = {}
    if (id(search.thread)) out.thread = id(search.thread)
    if (id(search.message)) out.message = id(search.message)
    if (search.people === true || search.people === 'true') out.people = true
    return out
  },
  loader: async (ctx) => {
    const path = `${workspacePath(ctx.params.workspace)}/chats/${encodeURIComponent(ctx.params.chatId)}`
    try {
      const [chat, page] = await Promise.all([loaderApi<Chat>(ctx, path), loaderApi<MessagePage>(ctx, `${path}/messages`)])
      return { chat, page }
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) throw notFound()
      throw e
    }
  },
  component: ChatPage,
  notFoundComponent: () => <p className="text-body-sm text-secondary text-center m-0 p-8">This chat doesn't exist, or you're not in it.</p>,
})

function ChatPage() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const { user } = Route.useRouteContext()
  const { chat, page } = Route.useLoaderData()
  const { thread, message, people } = Route.useSearch()
  return <ChatView key={chat.id} slug={workspace.slug} workspaceName={workspace.name} me={user} chat={chat} page={page} thread={thread} message={message} people={people} />
}
