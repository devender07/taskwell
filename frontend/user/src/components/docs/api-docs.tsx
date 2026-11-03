import { Link } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { Logo } from '@tether/shared/logo'
import { REFERENCE, type Endpoint } from './reference'
import './docs.css'

const NAV = [
  { id: 'authentication', title: 'Authentication' },
  { id: 'conventions', title: 'Conventions' },
  { id: 'reference', title: 'Reference' },
  ...REFERENCE.map((g) => ({ id: g.id, title: g.title, sub: true })),
  { id: 'webhooks', title: 'Webhooks' },
]

const PAYLOAD = `{
  "id": "0193f6c2-…",
  "type": "issue.updated",
  "created_at": "2026-11-03T09:30:00Z",
  "workspace": "acme",
  "actor": { "id": "…", "name": "Ana" },
  "issue": { "id": "…", "identifier": "ENG-12", "title": "Fix sign-in redirect" },
  "change": {
    "kind": "status",
    "data": { "from": { "id": "…", "name": "Todo" }, "to": { "id": "…", "name": "In progress" } }
  }
}`

const NODE = `import crypto from 'node:crypto'

// body: the raw request body, before JSON.parse. header: X-Tether-Signature.
export function verify(body, header, secret) {
  const parts = Object.fromEntries(header.split(',').map((p) => p.split('=')))
  const t = Number(parts.t)
  if (!t || Math.abs(Date.now() / 1000 - t) > 300) return false
  const expected = crypto.createHmac('sha256', secret).update(\`\${t}.\${body}\`).digest('hex')
  const a = Buffer.from(expected)
  const b = Buffer.from(parts.v1 ?? '')
  return a.length === b.length && crypto.timingSafeEqual(a, b)
}`

const GO = `import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

// body: the raw request body. header: X-Tether-Signature.
func verify(body []byte, header, secret string) bool {
	var t, sig string
	for _, part := range strings.Split(header, ",") {
		k, v, _ := strings.Cut(part, "=")
		switch k {
		case "t":
			t = v
		case "v1":
			sig = v
		}
	}
	ts, err := strconv.ParseInt(t, 10, 64)
	if err != nil || time.Since(time.Unix(ts, 0)).Abs() > 5*time.Minute {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(t + "."))
	mac.Write(body)
	return hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(sig))
}`

function Code({ children }: { children: string }) {
  return <pre className="docs-pre">{children}</pre>
}

function Section({ id, title, children }: { id: string; title: string; children: ReactNode }) {
  return (
    <section id={id} className="grid gap-4 docs-section" aria-labelledby={`${id}-title`}>
      <h2 id={`${id}-title`} className="text-h2 m-0">
        {title}
      </h2>
      {children}
    </section>
  )
}

function Table({ head, rows }: { head: string[]; rows: ReactNode[][] }) {
  return (
    <div className="overflow-x-auto">
      <table className="docs-table">
        <thead>
          <tr>
            {head.map((h) => (
              <th key={h}>{h}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => (
            <tr key={i}>
              {r.map((c, j) => (
                <td key={j}>{c}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function EndpointCard({ e }: { e: Endpoint }) {
  return (
    <article className="grid gap-3 p-4 bg-surface border rounded-lg">
      <h4 className="m-0 flex flex-wrap items-baseline gap-2 docs-mono">
        <span className="text-secondary">{e.method}</span>
        <span style={{ wordBreak: 'break-all' }}>{e.path}</span>
      </h4>
      <p className="text-body-sm m-0">{e.about}</p>
      {e.fields ? <Table head={['Field', 'Notes']} rows={e.fields.map(([f, n]) => [<code className="docs-mono">{f}</code>, n])} /> : null}
      <p className="text-caption text-secondary m-0">
        Returns <code className="docs-mono">{e.returns}</code>
      </p>
    </article>
  )
}

export function ApiDocs() {
  const origin = window.location.origin
  return (
    <div className="docs-layout">
      <header className="docs-header flex items-center gap-4 px-6">
        <Link to="/" aria-label="Tether home" className="flex">
          <Logo size={24} />
        </Link>
        <span className="text-secondary text-body-sm">API reference</span>
      </header>
      <nav className="docs-nav" aria-label="On this page">
        {NAV.map((n) => (
          <a key={n.id} href={`#${n.id}`} className={'sub' in n ? 'docs-nav-sub' : undefined}>
            {n.title}
          </a>
        ))}
      </nav>
      <main className="docs-main grid gap-12">
        <header className="grid gap-2">
          <h1 className="text-h2 m-0">Tether API</h1>
          <p className="text-body-sm text-secondary m-0">Read and change your workspace from scripts and services, and get events pushed to you with webhooks.</p>
        </header>

        <Section id="authentication" title="Authentication">
          <p className="text-body-sm m-0">
            Create a personal token in Settings → API tokens and send it on every request. A token acts as you: it sees and can do exactly what you can, in every workspace you belong to. Revoke it there when you no longer need it.
          </p>
          <Code>{`curl ${origin}/api/workspaces \\\n  -H "Authorization: Bearer tth_…"`}</Code>
          <p className="text-body-sm m-0">
            Call the API from a server or script. Write requests sent by a browser from another site are rejected with 403 "Cross-site request blocked."
          </p>
        </Section>

        <Section id="conventions" title="Conventions">
          <ul className="docs-list text-body-sm">
            <li>
              Base URL: <code className="docs-mono">{origin}/api</code>. Paths below are relative to it.
            </li>
            <li>Requests send form fields, as multipart/form-data or application/x-www-form-urlencoded. Lists repeat the field (label_ids=…&label_ids=…). Booleans are true or false.</li>
            <li>Responses are JSON. Times are RFC 3339 in UTC; dates are YYYY-MM-DD.</li>
            <li>
              Paths name the workspace by slug (<code className="docs-mono">acme</code>), projects by key (<code className="docs-mono">ENG</code>), issues by identifier (<code className="docs-mono">ENG-12</code>) and cycles by number.
            </li>
            <li>
              Rich text (issue descriptions, comments) is a ProseMirror document as a JSON string, e.g.{' '}
              <code className="docs-mono">{'{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Hi"}]}]}'}</code>.
            </li>
            <li>Lists that page take before (a cursor from the last item) or limit and offset, and answer has_more.</li>
          </ul>
          <Code>{`curl ${origin}/api/workspaces/acme/projects/ENG/issues \\\n  -H "Authorization: Bearer tth_…" \\\n  -F title="Fix sign-in redirect" -F priority=2`}</Code>
          <Table
            head={['Status', 'Meaning']}
            rows={[
              ['200, 201', 'Done. The body is the resource.'],
              ['202', 'Queued.'],
              ['204', 'Done, no body.'],
              ['400', <>Invalid fields: <code className="docs-mono">{'{"errors": {"title": "Title is required."}}'}</code></>],
              ['401', 'Missing, revoked or unknown token.'],
              ['403', <>Not allowed: <code className="docs-mono">{'{"error": "You don\'t have permission to do that."}'}</code></>],
              ['404', <>Not found, or you can't see it: <code className="docs-mono">{'{"error": "Issue not found."}'}</code></>],
              ['409', 'Conflicts with the current state, e.g. a taken project key.'],
              ['500', 'Something went wrong on our side. Retry later.'],
            ]}
          />
        </Section>

        <Section id="reference" title="Reference">
          <p className="text-body-sm text-secondary m-0">The endpoints most integrations need. Every path starts with {origin}/api.</p>
          {REFERENCE.map((g) => (
            <section key={g.id} id={g.id} className="grid gap-3 docs-section" aria-labelledby={`${g.id}-title`}>
              <h3 id={`${g.id}-title`} className="text-label m-0">
                {g.title}
              </h3>
              {g.intro ? <p className="text-body-sm text-secondary m-0">{g.intro}</p> : null}
              {g.endpoints.map((e) => (
                <EndpointCard key={e.method + e.path} e={e} />
              ))}
            </section>
          ))}
        </Section>

        <Section id="webhooks" title="Webhooks">
          <p className="text-body-sm m-0">
            Add a webhook in Settings → Webhooks (needs the manage integrations permission). Tether POSTs a JSON event to your HTTPS URL whenever one of its chosen events happens, from the app, the API, AI tools or a linked pull request.
          </p>
          <Table
            head={['Event', 'When']}
            rows={[
              [<code className="docs-mono">issue.created</code>, 'An issue is created.'],
              [<code className="docs-mono">issue.updated</code>, 'Any change is logged on an issue: status, assignee, priority, title, labels, due date, cycle, parent, links, test runs, pull requests. One delivery per change.'],
              [<code className="docs-mono">issue.deleted</code>, 'An issue is deleted. Its actor is always null.'],
              [<code className="docs-mono">comment.created</code>, 'Someone comments on an issue.'],
              [<code className="docs-mono">ping</code>, 'You press Send test.'],
            ]}
          />
          <p className="text-body-sm m-0">
            Description edits fire <code className="docs-mono">issue.updated</code> with kind description at most once per 10 minutes per person. Issues in private projects are never sent.
          </p>
          <h3 className="text-label m-0">Payload</h3>
          <Code>{PAYLOAD}</Code>
          <ul className="docs-list text-body-sm">
            <li>id is the delivery id; type is the event. actor is null when no person made the change.</li>
            <li>
              change is only on issue.updated: kind is the activity kind (status, assignee, priority, title, description, due_date, labels, parent, cycle, link, test_run, pr_linked …) and data its details.
            </li>
            <li>
              comment, <code className="docs-mono">{'{"id"}'}</code>, is only on comment.created. issue is absent on ping.
            </li>
            <li>
              Fetch the full issue with <code className="docs-mono">GET /api/workspaces/{'{slug}'}/issues/{'{identifier}'}</code> and an API token.
            </li>
          </ul>
          <h3 className="text-label m-0">Headers</h3>
          <Table
            head={['Header', 'Value']}
            rows={[
              ['Content-Type', 'application/json'],
              ['User-Agent', 'Tether-Webhooks/1'],
              ['X-Tether-Event', 'The event, e.g. issue.updated.'],
              ['X-Tether-Delivery', 'The delivery id. A redelivery has a new id.'],
              ['X-Tether-Signature', <code className="docs-mono">{'t=<unix seconds>,v1=<hex HMAC-SHA256 of "<t>.<body>">'}</code>],
            ]}
          />
          <h3 className="text-label m-0">Delivery and retries</h3>
          <ul className="docs-list text-body-sm">
            <li>Answer with any 2xx within 10 seconds. Redirects aren't followed.</li>
            <li>Anything else is retried after 1 minute, 5 minutes, 30 minutes, 2 hours, 6 hours and 12 hours. That is 7 attempts in all; when the last one fails, the delivery is marked failed.</li>
            <li>Settings → Webhooks shows the last 50 deliveries with their response, and can redeliver any of them. Deliveries are kept for 30 days.</li>
            <li>Turning a webhook off pauses it: deliveries wait and resume when it's turned back on.</li>
            <li>URLs must use https and can't point to private, loopback or link-local addresses.</li>
          </ul>
          <h3 id="signatures" className="text-label m-0">Verify signatures</h3>
          <p className="text-body-sm m-0">
            Each webhook has a signing secret (<code className="docs-mono">whsec_…</code>), shown once when you add it or roll it. Use the whole string, <code className="docs-mono">whsec_</code> included, as the HMAC key. Compute the HMAC over the raw body, compare in constant time, and reject old timestamps to stop replays.
          </p>
          <h4 className="text-body-sm text-secondary m-0">Node</h4>
          <Code>{NODE}</Code>
          <h4 className="text-body-sm text-secondary m-0">Go</h4>
          <Code>{GO}</Code>
        </Section>
      </main>
    </div>
  )
}
