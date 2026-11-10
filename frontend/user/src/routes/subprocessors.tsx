import { Link, createFileRoute } from '@tanstack/react-router'
import { LegalPage } from '@/components/legal/legal-page'

export const Route = createFileRoute('/subprocessors')({
  component: Subprocessors,
})

const ROWS = [
  ['Cloudflare', 'Hosting of the web app, file storage (R2), CDN, cookieless analytics', 'Global'],
  ['[VPS host]', 'API, database, backups', '[country]'],
  ['[SMTP provider]', 'Email delivery', '[country]'],
  ['Anthropic', 'AI testing, only when a workspace uses it. API data is not used for training.', 'United States'],
]

function Subprocessors() {
  return (
    <LegalPage title="Subprocessors">
      <p>
        These providers process personal data for us to run Taskwell. Each is bound by a data processing agreement, with standard contractual clauses
        or the EU-US Data Privacy Framework where data leaves India or the EU. See the <Link to="/privacy">privacy notice</Link> for what we
        collect.
      </p>
      <div className="legal-scroll">
        <table className="legal-table">
          <thead>
            <tr>
              <th scope="col">Provider</th>
              <th scope="col">Purpose</th>
              <th scope="col">Location</th>
            </tr>
          </thead>
          <tbody>
            {ROWS.map(([name, purpose, where]) => (
              <tr key={name}>
                <td>{name}</td>
                <td>{purpose}</td>
                <td>{where}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <h2>Not subprocessors</h2>
      <ul>
        <li>Paddle handles payments as an independent controller and merchant of record.</li>
        <li>Integrations you turn on (Google sign-in, GitHub, GitLab, webhooks) receive data at your request.</li>
      </ul>
      <p>
        We'll update this page before adding a new subprocessor. Questions: <a href="mailto:privacy@taskwell.dev">privacy@taskwell.dev</a>. See
        also the <Link to="/terms">terms of service</Link>.
      </p>
    </LegalPage>
  )
}
