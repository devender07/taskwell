import { createFileRoute } from '@tanstack/react-router'
import { Logo } from '@tether/shared/logo'

export const Route = createFileRoute('/')({
  component: () => (
    <main className="grid place-center min-h-screen">
      <Logo size={40} />
    </main>
  ),
})
