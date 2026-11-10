import { Link } from '@tanstack/react-router'
import { useState, useTransition, type ReactNode } from 'react'
import { CheckIcon } from '@taskwell/shared/icons'
import { dismissOnboarding, toFormErrors, type Onboarding, type OnboardingStep } from '@/api'
import { useShortcuts } from '@/components/app/shortcuts'
import { FormError } from '@/components/auth/auth-card'
import { Card } from './blocks'

const STEP = { color: 'var(--link)', textDecoration: 'none' }

const LABELS: Record<OnboardingStep['id'], string> = {
  project: 'Create a project',
  invite: 'Invite a teammate',
  issue: 'Create your first issue',
  github: 'Connect GitHub',
}

export function GettingStarted({ workspace, onboarding }: { workspace: string; onboarding: Onboarding }) {
  const { openCreate } = useShortcuts()
  const [hidden, setHidden] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [pending, startTransition] = useTransition()
  const done = onboarding.steps.filter((s) => s.done).length
  if (hidden || onboarding.dismissed || done === onboarding.steps.length) return null

  const dismiss = () =>
    startTransition(async () => {
      setError(null)
      try {
        await dismissOnboarding(workspace)
        setHidden(true)
      } catch (e) {
        setError(toFormErrors(e).error)
      }
    })

  const action = (id: OnboardingStep['id'], children: ReactNode) => {
    if (id === 'issue')
      return (
        <button type="button" className="home-link-button text-body-sm text-left" style={STEP} onClick={openCreate}>
          {children}
        </button>
      )
    const to = id === 'project' ? '/$workspace/projects' : id === 'invite' ? '/$workspace/settings/members' : '/$workspace/settings/integrations'
    return (
      <Link to={to} params={{ workspace }} className="text-body-sm" style={STEP}>
        {children}
      </Link>
    )
  }

  return (
    <Card
      title="Getting started"
      action={
        <button type="button" className="home-link home-link-button" disabled={pending} onClick={dismiss}>
          Dismiss
        </button>
      }
    >
      <p className="text-caption text-secondary m-0 px-6 pt-4">
        {done} of {onboarding.steps.length} done
      </p>
      <ul className="home-list px-6 py-2">
        {onboarding.steps.map((s) => (
          <li key={s.id} className="flex items-center gap-3" style={{ minHeight: 44 }}>
            {s.done ? (
              <CheckIcon className="text-success flex-noshrink" />
            ) : (
              <span className="flex-noshrink rounded-full" style={{ width: 18, height: 18, border: '1.75px solid var(--text-2)' }} aria-hidden />
            )}
            {s.done ? (
              <span className="text-body-sm text-secondary" style={{ textDecoration: 'line-through' }}>
                {LABELS[s.id]}
                <span className="sr-only"> (done)</span>
              </span>
            ) : (
              action(s.id, LABELS[s.id])
            )}
          </li>
        ))}
      </ul>
      {error ? (
        <div className="px-6 pb-4">
          <FormError>{error}</FormError>
        </div>
      ) : null}
    </Card>
  )
}
