import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { can, type AiUsage, type TestAccount, type TestSite, workspacePath } from '@/api'
import { loaderApi } from '@/realtime'
import { Accounts, Sites, Usage } from '@/components/settings/ai-testing'

export const Route = createFileRoute('/_app/$workspace/_main/settings/ai-testing')({
  loader: async (ctx) => {
    const path = `${workspacePath(ctx.params.workspace)}`
    const [usage, sites, accounts] = await Promise.all([
      loaderApi<AiUsage>(ctx, `${path}/ai-usage`),
      loaderApi<TestSite[]>(ctx, `${path}/test-sites`),
      loaderApi<TestAccount[]>(ctx, `${path}/test-accounts`),
    ])
    return { usage, sites, accounts }
  },
  component: AiTestingSettings,
})

function AiTestingSettings() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const { usage, sites, accounts } = Route.useLoaderData()
  const manage = can(workspace, 'ai_tests.manage')
  return (
    <main className="grid gap-8 w-full px-8 py-10" style={{ alignContent: 'start' }}>
      <header className="page-top grid gap-2">
        <h1 className="text-h2">AI testing</h1>
        <p className="text-body-sm text-secondary">Where AI tests run, the accounts they sign in with, and this month's spend.</p>
      </header>
      <Usage usage={usage} />
      <Sites workspace={workspace} sites={sites} manage={manage} />
      <Accounts workspace={workspace} accounts={accounts} sites={sites} manage={manage} />
      {manage ? null : <p className="text-caption text-secondary">You don't have permission to change AI testing settings.</p>}
    </main>
  )
}
