import { createFileRoute } from '@tanstack/react-router'
import { AvatarForm } from '@/components/settings/avatar-form'
import { EmailForm } from '@/components/settings/email-form'
import { PasswordForm } from '@/components/settings/password-form'
import { ProfileForm } from '@/components/settings/profile-form'
import { ShortcutsSetting } from '@/components/settings/shortcuts-setting'

export const Route = createFileRoute('/_app/$workspace/_main/settings/profile')({
  component: ProfileSettings,
})

function ProfileSettings() {
  const { user } = Route.useRouteContext()
  return (
    <main className="grid gap-6 w-full px-8 py-10" style={{ alignContent: 'start' }}>
      <header className="grid gap-2">
        <h1 className="text-h2">Profile</h1>
        <p className="text-body-sm text-secondary">How you appear to people in your workspaces.</p>
      </header>
      <section className="grid gap-4 p-6 bg-surface border rounded-lg">
        <AvatarForm user={user} />
      </section>
      <section className="grid gap-4 p-6 bg-surface border rounded-lg">
        <ProfileForm key={user.id} user={user} />
      </section>
      <section className="grid gap-4 p-6 bg-surface border rounded-lg">
        <EmailForm key={user.id} user={user} />
      </section>
      <section className="grid gap-4 p-6 bg-surface border rounded-lg">
        <PasswordForm key={user.id} user={user} />
      </section>
      <section className="grid gap-4 p-6 bg-surface border rounded-lg">
        <ShortcutsSetting />
      </section>
    </main>
  )
}
