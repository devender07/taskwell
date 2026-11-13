import { Outlet, createFileRoute } from '@tanstack/react-router'
import { SettingsNav, SettingsSheet } from '@/components/settings/settings-nav'

export const Route = createFileRoute('/_app/$workspace/_main/settings')({
  component: SettingsLayout,
})

function SettingsLayout() {
  const { workspace } = Route.useParams()
  return (
    <div className="settings-layout">
      <SettingsNav workspace={workspace} />
      <div>
        <SettingsSheet workspace={workspace} />
        <Outlet />
      </div>
    </div>
  )
}
