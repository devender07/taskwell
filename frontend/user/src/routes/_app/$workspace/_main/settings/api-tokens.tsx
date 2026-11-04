import { Link, createFileRoute } from '@tanstack/react-router'
import { tokensPath, type ApiToken } from '@/api'
import { loaderApi } from '@/realtime'
import { NewTokenButton, TokenRow } from '@/components/settings/api-tokens'

export const Route = createFileRoute('/_app/$workspace/_main/settings/api-tokens')({
  loader: (ctx) => loaderApi<ApiToken[]>(ctx, tokensPath),
  component: ApiTokensSettings,
})

function ApiTokensSettings() {
  const tokens = Route.useLoaderData()
  return (
    <main className="grid gap-6 w-full px-8 py-10" style={{ alignContent: 'start' }}>
      <header className="grid gap-2">
        <h1 className="text-h2">API tokens</h1>
        <p className="text-body-sm text-secondary">Tokens let AI tools like Claude Code act as you in Taskwell. Anyone with a token can do what you can.{' '}
          <Link to="/docs/api" style={{ color: 'var(--link)' }}>
            Read the API docs
          </Link>
        </p>
      </header>
      <NewTokenButton />
      {tokens.length ? (
        <ul className="grid m-0 p-0 bg-surface border rounded-lg" style={{ listStyle: 'none' }} aria-label="API tokens">
          {tokens.map((t) => (
            <TokenRow key={t.id} token={t} />
          ))}
        </ul>
      ) : (
        <p className="text-body-sm text-secondary">No tokens yet.</p>
      )}
    </main>
  )
}
