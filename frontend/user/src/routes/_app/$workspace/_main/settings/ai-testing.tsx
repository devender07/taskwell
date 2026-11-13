import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { can, serverRuns, type AiUsage, type TestAccount, type TestSite, workspacePath, requireFeature } from '@/api'
import { loaderApi } from '@/realtime'
import { Accounts, Sites, Usage } from '@/components/settings/ai-testing'
import { RunnerSetup } from '@/components/tests/local-run'

export const Route = createFileRoute('/_app/$workspace/_main/settings/ai-testing')({
  beforeLoad: requireFeature('ai_testing'),
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
  const server = serverRuns(usage)
  return (
    <main className="grid gap-8 w-full px-8 py-10" style={{ alignContent: 'start' }}>
      <header className="page-top grid gap-2">
        <h1 className="text-h2">AI testing</h1>
        <p className="text-body-sm text-secondary">
          {server ? "Where AI tests run, the accounts they sign in with, and this month's spend." : 'Tests run on your machine, driven by your own coding agent.'}
        </p>
      </header>
      <section className="grid gap-4 p-6 bg-surface border rounded-lg" aria-labelledby="local-runner-heading">
        <div className="grid gap-1">
          <h2 id="local-runner-heading" className="text-label">Run on your machine</h2>
          <p className="text-caption text-secondary m-0">
            taskwell-tester lets your coding agent test an issue in Chrome on your machine, localhost included, and sends every step, screenshot and video to the issue.
          </p>
        </div>
        <RunnerSetup slug={workspace.slug} label="Add it to Claude Code" />
      </section>
      {server ? (
        <>
          <Usage usage={usage} />
          <Sites workspace={workspace} sites={sites} manage={manage} />
          <Accounts workspace={workspace} accounts={accounts} sites={sites} manage={manage} />
          {manage ? null : <p className="text-caption text-secondary">You don't have permission to change AI testing settings.</p>}
        </>
      ) : null}
    </main>
  )
}
