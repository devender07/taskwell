import { useActionState } from 'react'
import { Button } from '@tether/shared/button'
import { Input } from '@tether/shared/input'
import { api, toFormErrors, type FieldErrors, type Setting } from '@/api'

type State = { setting: Setting; value: string; saved?: boolean; errors?: FieldErrors; error?: string | null }

export function SettingRow({ setting: initial }: { setting: Setting }) {
  const [state, submit, pending] = useActionState(async (prev: State, form: FormData): Promise<State> => {
    const value = String(form.get('value') ?? '')
    if (prev.setting.secret && !value) return { setting: prev.setting, value }
    try {
      const setting = await api<Setting>(`/config/${encodeURIComponent(prev.setting.key)}`, form)
      return { setting, value: setting.value, saved: true }
    } catch (e) {
      return { setting: prev.setting, value, ...toFormErrors(e) }
    }
  }, { setting: initial, value: initial.value })

  const { setting } = state
  const id = `setting-${setting.key}`
  const locked = setting.key === 'jwt_secret'

  return (
    <form action={submit} noValidate className="setting">
      <div className="grid gap-1" style={{ minWidth: 0 }}>
        <label htmlFor={id} className="text-label" style={{ fontFamily: 'var(--mono)', overflowWrap: 'anywhere' }}>
          {setting.key}
        </label>
        {setting.description ? <p className="text-caption text-secondary">{setting.description}</p> : null}
      </div>
      {locked ? (
        <p className="text-caption text-secondary" style={{ gridColumn: 'span 2', alignSelf: 'center' }}>
          {setting.has_value ? 'Set. ' : ''}This setting can't be changed here.
        </p>
      ) : (
        <>
          <div className="grid gap-1" style={{ minWidth: 0 }}>
            <Input
              id={id}
              name="value"
              type={setting.secret ? 'password' : 'text'}
              autoComplete={setting.secret ? 'new-password' : 'off'}
              placeholder={setting.secret ? (setting.has_value ? '••••••' : 'Not set') : undefined}
              defaultValue={state.value}
              error={state.errors?.value ?? state.error}
            />
            {state.saved && !pending ? <span role="status" className="text-caption text-success">Saved</span> : null}
          </div>
          <Button type="submit" disabled={pending}>
            {pending ? 'Saving…' : 'Save'}
          </Button>
        </>
      )}
    </form>
  )
}
