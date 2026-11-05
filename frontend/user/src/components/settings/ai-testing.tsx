import { useRouter } from '@tanstack/react-router'
import { useActionState, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Input } from '@taskwell/shared/input'
import { SmartAutocomplete, type Option } from '@taskwell/shared/smart-autocomplete'
import { testAccountLabel, testAccountPassword, testAccountUsername, testSiteUrl } from '@taskwell/shared/validate'
import { api, testAccountsPath, testSitesPath, toFormErrors, type AiUsage, type FieldErrors, type TestAccount, type TestSite, type Workspace } from '@/api'
import { reloadPage } from '@/realtime'
import { FormError } from '@/components/auth/auth-card'
import { formatDate } from '@/components/issues/fields'
import { ConfirmButton, formatCost } from '@/components/tests/tests'

type State = { errors?: FieldErrors; error?: string | null }

export function Usage({ usage }: { usage: AiUsage }) {
  const share = usage.limit_usd > 0 ? Math.min(1, usage.spent_usd / usage.limit_usd) : 1
  return (
    <section className="grid gap-3 p-6 bg-surface border rounded-lg" aria-labelledby="ai-usage-heading">
      <h2 id="ai-usage-heading" className="text-label">This month</h2>
      <p className="text-body-sm m-0">
        {formatCost(usage.spent_usd)} of {formatCost(usage.limit_usd)} spent
      </p>
      <div
        role="meter"
        aria-label="AI test spend this month"
        aria-valuemin={0}
        aria-valuemax={usage.limit_usd}
        aria-valuenow={usage.spent_usd}
        className="rounded-full overflow-hidden"
        style={{ height: 8, background: 'rgba(var(--ink), 0.05)' }}
      >
        <div className="h-full rounded-full" style={{ width: `${share * 100}%`, background: share >= 0.9 ? 'var(--accent)' : 'var(--text-2)' }} />
      </div>
      <p className="text-caption text-secondary m-0">
        {share >= 1 ? 'The limit is reached; AI tests stop until it resets. ' : ''}Resets {formatDate(usage.resets_at)}.
      </p>
    </section>
  )
}

function AddSite({ workspace }: { workspace: Workspace }) {
  const router = useRouter()
  const [url, setUrl] = useState('')
  const [{ errors, error }, submit, pending] = useActionState(async (): Promise<State> => {
    const errors = { base_url: testSiteUrl(url) }
    if (errors.base_url) return { errors }
    const form = new FormData()
    form.set('base_url', url.trim())
    try {
      await api<TestSite>(testSitesPath(workspace), form)
    } catch (e) {
      return toFormErrors(e)
    }
    await reloadPage(router)
    setUrl('')
    return {}
  }, {})
  return (
    <form action={submit} noValidate className="grid gap-2">
      <div className="flex flex-wrap items-start gap-3">
        <div className="flex-grow" style={{ minWidth: 200 }}>
          <Input aria-label="Site URL" placeholder="https://staging.example.com" autoComplete="off" value={url} onChange={(e) => setUrl(e.target.value)} error={errors?.base_url} />
        </div>
        <Button type="submit" disabled={pending}>
          {pending ? 'Adding…' : 'Add site'}
        </Button>
      </div>
      <FormError>{error}</FormError>
    </form>
  )
}

export function Sites({ workspace, sites, manage }: { workspace: Workspace; sites: TestSite[]; manage: boolean }) {
  const router = useRouter()
  return (
    <section className="grid gap-4" aria-labelledby="test-sites-heading">
      <div className="grid gap-1">
        <h2 id="test-sites-heading" className="text-label">Test sites</h2>
        <p className="text-caption text-secondary m-0">The browser can only open these hosts and their subdomains.</p>
      </div>
      {manage ? <AddSite workspace={workspace} /> : null}
      {sites.length === 0 ? (
        <p className="text-body-sm text-secondary p-6 bg-surface border rounded-lg m-0">No test sites yet.</p>
      ) : (
        <ul className="grid m-0 p-0 bg-surface border rounded-lg" style={{ listStyle: 'none' }} aria-label="Test sites">
          {sites.map((s) => (
            <li key={s.id} className="flex items-center gap-3 px-6 py-2" style={{ minHeight: 56 }}>
              <span className="text-label text-primary ellipsis flex-grow" style={{ minWidth: 0 }}>{s.base_url}</span>
              {manage ? (
                <ConfirmButton
                  label="Remove"
                  title={`Remove ${s.host}?`}
                  description="AI tests can't open it after this, and its test accounts are removed too."
                  confirm="Remove site"
                  pending="Removing…"
                  onConfirm={async () => {
                    await api(`${testSitesPath(workspace)}/${encodeURIComponent(s.id)}`, undefined, 'DELETE')
                    await reloadPage(router)
                  }}
                />
              ) : null}
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

const anySite: Option = { value: '', label: 'Any site' }

const siteOptions = (sites: TestSite[]): Option[] => [anySite, ...sites.map((s) => ({ value: s.id, label: s.base_url }))]

const siteName = (sites: TestSite[], id: string | null) => (id ? (sites.find((s) => s.id === id)?.base_url ?? 'Removed site') : 'Any site')

function AccountForm({ workspace, sites, account, onDone }: { workspace: Workspace; sites: TestSite[]; account?: TestAccount; onDone?: () => void }) {
  const router = useRouter()
  const options = siteOptions(sites)
  const [site, setSite] = useState<Option | null>(options.find((o) => o.value === (account?.site_id ?? '')) ?? anySite)
  const [label, setLabel] = useState(account?.label ?? '')
  const [username, setUsername] = useState(account?.username ?? '')
  const [password, setPassword] = useState('')

  const [{ errors, error }, submit, pending] = useActionState(async (): Promise<State> => {
    const errors = { label: testAccountLabel(label), username: testAccountUsername(username), password: account ? null : testAccountPassword(password) }
    if (Object.values(errors).some(Boolean)) return { errors }
    const form = new FormData()
    form.set('label', label.trim())
    form.set('username', username.trim())
    form.set('password', password)
    form.set('site_id', site?.value ?? '')
    try {
      await api<TestAccount>(account ? `${testAccountsPath(workspace)}/${encodeURIComponent(account.id)}` : testAccountsPath(workspace), form)
    } catch (e) {
      return toFormErrors(e)
    }
    await reloadPage(router)
    setPassword('')
    if (!account) {
      setLabel('')
      setUsername('')
      setSite(anySite)
    }
    onDone?.()
    return {}
  }, {})

  return (
    <form action={submit} noValidate className="grid gap-3">
      <div className="grid gap-3" style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(160px, 1fr))' }}>
        <Input label="Label" placeholder="Admin user" autoComplete="off" autoFocus={Boolean(account)} value={label} onChange={(e) => setLabel(e.target.value)} error={errors?.label} />
        <Input label="Username" placeholder="admin@example.com" autoComplete="off" value={username} onChange={(e) => setUsername(e.target.value)} error={errors?.username} />
        <Input
          label="Password"
          type="password"
          autoComplete="new-password"
          placeholder={account ? 'Saved; leave blank to keep' : undefined}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          error={errors?.password}
        />
        <SmartAutocomplete label="Site" options={options} value={site} onChange={(o) => setSite(o ?? anySite)} error={errors?.site_id} />
      </div>
      <FormError>{error}</FormError>
      <div className="flex items-center gap-3">
        <Button type="submit" disabled={pending}>
          {account ? (pending ? 'Saving…' : 'Save') : pending ? 'Adding…' : 'Add account'}
        </Button>
        {account ? (
          <Button variant="ghost" onClick={onDone} disabled={pending}>
            Cancel
          </Button>
        ) : null}
      </div>
    </form>
  )
}

function AccountRow({ workspace, sites, account, manage }: { workspace: Workspace; sites: TestSite[]; account: TestAccount; manage: boolean }) {
  const router = useRouter()
  const [editing, setEditing] = useState(false)
  if (editing) {
    return (
      <li className="px-6 py-4">
        <AccountForm workspace={workspace} sites={sites} account={account} onDone={() => setEditing(false)} />
      </li>
    )
  }
  return (
    <li className="flex flex-wrap items-center gap-3 px-6 py-2" style={{ minHeight: 56 }}>
      <div className="grid flex-grow" style={{ minWidth: 0 }}>
        <span className="text-label text-primary ellipsis">{account.label}</span>
        <span className="text-caption text-secondary ellipsis">
          {account.username} · {siteName(sites, account.site_id)}
        </span>
      </div>
      {manage ? (
        <>
          <Button variant="ghost" onClick={() => setEditing(true)}>
            Edit
          </Button>
          <ConfirmButton
            label="Remove"
            title={`Remove ${account.label}?`}
            description="AI tests can't sign in with it after this."
            confirm="Remove account"
            pending="Removing…"
            onConfirm={async () => {
              await api(`${testAccountsPath(workspace)}/${encodeURIComponent(account.id)}`, undefined, 'DELETE')
              await reloadPage(router)
            }}
          />
        </>
      ) : null}
    </li>
  )
}

export function Accounts({ workspace, accounts, sites, manage }: { workspace: Workspace; accounts: TestAccount[]; sites: TestSite[]; manage: boolean }) {
  return (
    <section className="grid gap-4" aria-labelledby="test-accounts-heading">
      <div className="grid gap-1">
        <h2 id="test-accounts-heading" className="text-label">Test accounts</h2>
        <p className="text-caption text-secondary m-0">
          Claude sees the label and username. The password is typed by the test runner and never shown again, and only on the account's site when it has one.
        </p>
      </div>
      {manage ? (
        <div className="p-6 bg-surface border rounded-lg">
          <AccountForm workspace={workspace} sites={sites} />
        </div>
      ) : null}
      {accounts.length === 0 ? (
        <p className="text-body-sm text-secondary p-6 bg-surface border rounded-lg m-0">No test accounts yet.</p>
      ) : (
        <ul className="grid m-0 p-0 bg-surface border rounded-lg" style={{ listStyle: 'none' }} aria-label="Test accounts">
          {accounts.map((a) => (
            <AccountRow key={a.id} workspace={workspace} sites={sites} account={a} manage={manage} />
          ))}
        </ul>
      )}
    </section>
  )
}
