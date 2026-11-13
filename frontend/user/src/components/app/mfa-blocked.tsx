import { Link, useRouteContext } from '@tanstack/react-router'
import { Button } from '@taskwell/shared/button'
import { LockIcon } from '@taskwell/shared/icons'
import { Popover } from '@taskwell/shared/popover'
import type { Workspace } from '@/api'

// Shown instead of the workspace's pages while it requires two-factor and the user has none.
export function MfaBlocked({ workspace }: { workspace: Workspace }) {
  const { workspaces, user } = useRouteContext({ from: '/_app' })
  const others = workspaces.filter((w) => w.slug !== workspace.slug)
  return (
    <main className="grid gap-6 w-full px-4 py-10" style={{ justifyItems: 'center', alignContent: 'center', minHeight: 'calc(100dvh - 56px)' }}>
      <section className="grid gap-4 p-6 bg-surface border rounded-lg w-full text-center" style={{ maxWidth: 440, justifyItems: 'center' }}>
        <LockIcon size={24} strokeWidth={1.5} className="text-secondary" />
        <div className="grid gap-2">
          <h1 className="text-h2">{workspace.name} requires two-factor sign-in</h1>
          <p className="text-body-sm text-secondary m-0">Turn it on to keep working here.</p>
        </div>
        <div className="flex flex-wrap justify-center gap-3">
          <Link to="/$workspace/settings/profile" params={{ workspace: workspace.slug }} hash="two-factor" className="button button-primary">
            Set up two-factor
          </Link>
          {others.length ? (
            <Popover
              aria-label="Workspaces"
              placement="bottom-start"
              trigger={(props) => (
                <Button variant="secondary" {...props}>
                  Switch workspace
                </Button>
              )}
            >
              {(close) => (
                <nav style={{ width: 264 }}>
                  {others.map((w) => (
                    <Link key={w.id} to="/$workspace" params={{ workspace: w.slug }} preload={false} className="popover-item" onClick={close}>
                      <span className="ellipsis">{w.name}</span>
                    </Link>
                  ))}
                </nav>
              )}
            </Popover>
          ) : user.can_create_workspaces ? (
            <Link to="/new-workspace" className="button button-secondary">
              Create a workspace
            </Link>
          ) : null}
        </div>
      </section>
    </main>
  )
}
